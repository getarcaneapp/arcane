package imageupdate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.uber.org/fx"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/cache"
	"kit.getarcane.app/concurrency"
	kitregistry "kit.getarcane.app/registry"
	"kit.getarcane.app/sys/crypto"
	"kit.getarcane.app/updater/digest"
	"kit.getarcane.app/updater/refs"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/imageupdate/children/releasenotes"
	"getarcane.dev/arcane/v3/internal/imageupdate/children/tags"
	"getarcane.dev/arcane/v3/internal/notification"
	registryInternal "getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/settings"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/envcontext"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/imageref"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/arcane/v3/pkg/registry"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	ImageUpdateService struct {
		db                  *database.DB
		settingsService     *settings.SettingsService
		registryService     *registryInternal.ContainerRegistryService
		dockerService       *docker.DockerClientService
		eventService        *event.EventService
		notificationService *notification.NotificationService
		registryLimiter     *registry.RegistryRateLimiter
		tags                *tags.Service
		releaseNotes        *releasenotes.Service
		activityService     *activity.ActivityService
		engine              *flow.Engine
		checkWorkflow       *flow.Workflow
		credentials         *cache.Cache[string, []types.ContainerRegistryCredential]
		// requestCredentials keeps caller credentials out of the workflow journal; they expire with the check's timeout.
		requestCredentials *cache.Cache[string, []types.ContainerRegistryCredential]
		composeBuildRefs   *cache.Cache[string, map[string]struct{}]
		releaseLinks       *cache.Cache[string, string]
		activityBatches    map[string][]activity.AppendActivityMessageRequest
		activityTimers     map[string]*time.Timer
		activityFlushLocks concurrency.KeyedMutex
		activityFlushWG    sync.WaitGroup
		notifyWG           sync.WaitGroup
		notifyStopMu       sync.RWMutex
		notifyMu           sync.Mutex
		activityBatchMu    sync.Mutex
		changePending      atomic.Bool
		activityStopped    bool
		notifyStopped      bool
	}

	batchChangeCoalesceKey struct{}

	ImageParts struct {
		Registry   string
		Repository string
		Tag        string
	}

	// imageCheckResult is a check task's output; failures travel as errors.
	imageCheckResult struct {
		// Result is the ref's own result, kept when the caller reports results; tags sharing an image share its stored row.
		Result *types.ImageUpdateResponse `json:"result,omitempty"`
		// Failure is a reported check's error, which keeps its Result instead of failing the slot.
		Failure string `json:"failure,omitempty"`
		// Digest is the remote digest resolved during the check step for tag-policy reuse.
		Digest string `json:"digest,omitempty"`
		Local  bool   `json:"local,omitempty"`
	}

	// imageCheckInput is an image-check payload; Report keeps every ref's result in the output for the caller.
	imageCheckInput struct {
		CredentialsKey string `json:"credentialsKey,omitempty"`
		types.ImageUpdateCheckRequest
		Report bool `json:"report,omitempty"`
	}

	// imageCheckOutput is the check outcome plus, when reported, every ref and container tag result.
	imageCheckOutput struct {
		Results          types.ImageUpdateBatchResponse        `json:"results,omitempty"`
		ContainerUpdates map[string]*types.ImageUpdateResponse `json:"containerUpdates,omitempty"`
		types.SchedulerOutcome
	}

	// tagCheckOutput is the tags step's output: failed container checks and, when reported, every container's result.
	tagCheckOutput struct {
		Updates  map[string]*types.ImageUpdateResponse `json:"updates,omitempty"`
		Failures []types.TargetOutcome                 `json:"failures"`
	}

	localImageSnapshot struct {
		Created           time.Time
		Platform          v1.Platform
		ImageID           string
		Repository        string
		Tag               string
		PrimaryDigest     string
		Version           string
		AllDigests        []string
		RepositoryDigests []string
		RepoTags          []string
		IsLocalBuild      bool
	}

	// monitoringEligibility records whether any consumer still permits update checks; only the update-check label counts.
	monitoringEligibility struct {
		// Present but false when every consumer of the image ID or reference opted out.
		byImageID   map[string]bool
		byRef       map[string]bool
		byContainer map[string]bool
	}

	ImageUpdateServiceParams struct {
		fx.In

		DB                  *database.DB
		SettingsService     *settings.SettingsService
		RegistryService     *registryInternal.ContainerRegistryService
		DockerService       *docker.DockerClientService
		EventService        *event.EventService
		NotificationService *notification.NotificationService
		ActivityService     *activity.ActivityService
	}
)

const (
	imageCheckConcurrency        = 24
	imageCheckAttempts           = 3
	staleDigestRecordDeleteChunk = 500
	releaseLinkConcurrency       = 8
	releaseLinkCacheCapacity     = 512
	releaseLinkCacheTTL          = 10 * time.Minute
	activityMessageBatchSize     = 16
	activityMessageFlushInterval = 250 * time.Millisecond
	activityMessageWriteTimeout  = 30 * time.Second
	imageUpdateChangeInterval    = time.Second
)

var (
	// errCheckInterrupted fails the images of a manual check that a restart interrupted.
	errCheckInterrupted       = errors.New("image check was interrupted by a restart; run it again")
	imageCheckRetryBackoff    = 30 * time.Second
	imageCheckRetryBackoffMax = 2 * time.Minute
)

func NewImageUpdateService(p ImageUpdateServiceParams) *ImageUpdateService {
	registryLimiter := registry.NewRegistryRateLimiter()
	return &ImageUpdateService{
		db:                  p.DB,
		settingsService:     p.SettingsService,
		registryService:     p.RegistryService,
		dockerService:       p.DockerService,
		eventService:        p.EventService,
		notificationService: p.NotificationService,
		registryLimiter:     registryLimiter,
		tags:                tags.NewService(p.RegistryService, p.DockerService, p.SettingsService, registryLimiter),
		releaseNotes:        releasenotes.NewService(p.DockerService, p.SettingsService, p.RegistryService.ImageConfig),
		activityService:     p.ActivityService,
		credentials: &cache.Cache[string, []types.ContainerRegistryCredential]{
			Capacity: 8,
			TTL:      time.Minute,
		},
		// They outlive the longest check: its slot wait plus its workflow timeout.
		requestCredentials: &cache.Cache[string, []types.ContainerRegistryCredential]{
			Capacity: 64,
			TTL:      timeouts.DefaultActivitySlotWait + timeouts.DefaultImageUpdateScan,
		},
		composeBuildRefs: &cache.Cache[string, map[string]struct{}]{
			Capacity: 8,
			TTL:      timeouts.DefaultImageUpdateScan,
		},
		releaseLinks: &cache.Cache[string, string]{
			Capacity: releaseLinkCacheCapacity,
			TTL:      releaseLinkCacheTTL,
		},
		activityBatches: map[string][]activity.AppendActivityMessageRequest{},
		activityTimers:  map[string]*time.Timer{},
	}
}

func (s *ImageUpdateService) Stop(ctx context.Context) error {
	s.activityBatchMu.Lock()
	s.activityStopped = true
	activityIDs := slices.Collect(maps.Keys(s.activityBatches))
	for id, timer := range s.activityTimers {
		if timer.Stop() {
			s.activityFlushWG.Done()
		}
		delete(s.activityTimers, id)
	}
	s.activityBatchMu.Unlock()
	for _, activityID := range activityIDs {
		s.flushActivityMessages(ctx, activityID)
	}
	flushErr := concurrency.WaitGroup(ctx, &s.activityFlushWG)

	if s.changePending.Swap(false) {
		s.eventService.PublishChange("0", "image_update")
	}

	s.notifyStopMu.Lock()
	s.notifyStopped = true
	s.notifyStopMu.Unlock()
	return errors.Join(flushErr, concurrency.WaitGroup(ctx, &s.notifyWG))
}

func (s *ImageUpdateService) composeBuildImageRefs(ctx context.Context) (map[string]struct{}, error) {
	var projectRows []struct {
		BuildImageRefsJSON *string `gorm:"column:build_image_refs_json"`
		ID                 string
	}
	if err := s.db.WithContext(ctx).
		Table("projects").
		Select("id", "build_image_refs_json").
		Where("build_image_refs_json IS NOT NULL AND build_image_refs_json <> ''").
		Find(&projectRows).Error; err != nil {
		return nil, fmt.Errorf("load project build image references: %w", err)
	}

	// Copacetic-patched tags exist only locally, so treat them as local builds; patch history never fails a check.
	var patchedRefs []string
	if err := s.db.WithContext(ctx).
		Table("image_patches").
		Where("status = ?", "completed").
		Distinct().
		Pluck("patched_ref", &patchedRefs).Error; err != nil {
		slog.DebugContext(ctx, "failed to load patched image references", "error", err)
	}
	for _, row := range projectRows {
		patchedRefs = append(patchedRefs, projects.ParseImageRefsJSON(kit.FromPtr(row.BuildImageRefsJSON))...)
	}

	buildRefs := make(map[string]struct{}, len(patchedRefs))
	for _, imageRef := range patchedRefs {
		if normalized := refs.NormalizeImageUpdateRef(imageRef); normalized != "" {
			buildRefs[normalized] = struct{}{}
		}
	}

	return buildRefs, nil
}

// appendImageUpdateActivityMessage leaves progress to the activity's owner when progress is nil.
func (s *ImageUpdateService) appendImageUpdateActivityMessage(ctx context.Context, activityID, message string, progress *int, step string) {
	if activityID == "" || strings.TrimSpace(message) == "" {
		return
	}
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    types.ActivityMessageLevelInfo,
		Message:  message,
		Progress: progress,
		Step:     step,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append image update activity message", "activityId", activityID, "error", err)
	}
}

func (s *ImageUpdateService) queueActivityMessage(ctx context.Context, activityID string, level types.ActivityMessageLevel, message string) {
	if activityID == "" {
		return
	}
	req := activity.AppendActivityMessageRequest{
		Level:   level,
		Message: message,
	}

	s.activityBatchMu.Lock()
	if s.activityStopped {
		s.activityBatchMu.Unlock()
		s.writeActivityMessageBatch(ctx, activityID, []activity.AppendActivityMessageRequest{req})
		return
	}
	s.activityBatches[activityID] = append(s.activityBatches[activityID], req)
	shouldFlush := len(s.activityBatches[activityID]) >= activityMessageBatchSize
	if shouldFlush {
		if timer := s.activityTimers[activityID]; timer != nil {
			if timer.Stop() {
				s.activityFlushWG.Done()
			}
			delete(s.activityTimers, activityID)
		}
	} else if s.activityTimers[activityID] == nil {
		writeCtx := context.WithoutCancel(ctx)
		s.activityFlushWG.Add(1)
		s.activityTimers[activityID] = time.AfterFunc(activityMessageFlushInterval, func() {
			defer s.activityFlushWG.Done()
			s.flushActivityMessages(writeCtx, activityID)
		})
	}
	s.activityBatchMu.Unlock()

	if shouldFlush {
		s.flushActivityMessages(ctx, activityID)
	}
}

func (s *ImageUpdateService) flushActivityMessages(ctx context.Context, activityID string) {
	if activityID == "" {
		return
	}
	defer s.activityFlushLocks.Lock(activityID)()

	for {
		s.activityBatchMu.Lock()
		if timer := s.activityTimers[activityID]; timer != nil {
			if timer.Stop() {
				s.activityFlushWG.Done()
			}
			delete(s.activityTimers, activityID)
		}
		batch := s.activityBatches[activityID]
		delete(s.activityBatches, activityID)
		tracked := len(batch) > 0 && !s.activityStopped
		if tracked {
			s.activityFlushWG.Add(1)
		}
		s.activityBatchMu.Unlock()
		if len(batch) == 0 {
			return
		}
		s.writeActivityMessageBatch(ctx, activityID, batch)
		if tracked {
			s.activityFlushWG.Done()
		}
	}
}

func (s *ImageUpdateService) writeActivityMessageBatch(ctx context.Context, activityID string, batch []activity.AppendActivityMessageRequest) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), activityMessageWriteTimeout)
	defer cancel()
	if _, err := s.activityService.AppendMessages(writeCtx, activityID, batch); err != nil {
		slog.DebugContext(writeCtx, "failed to append image update activity message batch", "activityId", activityID, "count", len(batch), "error", err)
	}
}

func (s *ImageUpdateService) publishImageUpdateChange(ctx context.Context) {
	environmentID := envcontext.EnvironmentIDOrDefault(ctx)
	if batch, _ := ctx.Value(batchChangeCoalesceKey{}).(bool); !batch {
		s.eventService.PublishChange(environmentID, "image_update")
		return
	}
	if s.changePending.CompareAndSwap(false, true) {
		time.AfterFunc(imageUpdateChangeInterval, func() {
			if s.changePending.Swap(false) {
				s.eventService.PublishChange(environmentID, "image_update")
			}
		})
	}
}

func (s *ImageUpdateService) completeImageUpdateActivity(ctx context.Context, activityID string, success bool, message string) {
	if activityID == "" {
		return
	}
	status, errMessage, finalMessage := func() (types.ActivityStatus, *string, string) {
		switch {
		case success:
			return types.ActivityStatusSuccess, nil, cmp.Or(message, "Image update check completed")
		case errors.Is(context.Cause(ctx), activitylib.ErrCanceled):
			return types.ActivityStatusCancelled, nil, "Image update check cancelled"
		default:
			return types.ActivityStatusFailed, mo.EmptyableToOption(strings.TrimSpace(message)).ToPointer(), cmp.Or(message, "Image update check completed")
		}
	}()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := s.activityService.CompleteActivity(writeCtx, activityID, status, finalMessage, errMessage, "Image update check complete"); err != nil {
		// A lost terminal write strands the activity in running, so log loudly.
		slog.ErrorContext(ctx, "failed to complete image update activity", "activityId", activityID, "error", err)
	}
}

func (s *ImageUpdateService) CheckImageUpdate(ctx context.Context, imageRef string) (*types.ImageUpdateResponse, error) {
	startTime := time.Now()
	activityID := func() string {
		localActivity, err := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
			EnvironmentID: envcontext.EnvironmentIDOrDefault(ctx),
			Type:          types.ActivityTypeImageUpdateCheck,
			Queue:         true,
			ResourceType:  new("image"),
			ResourceName:  mo.EmptyableToOption(strings.TrimSpace(imageRef)).ToPointer(),
			Step:          "Checking image updates",
			LatestMessage: "Image update check started",
			Metadata: database.JSON{
				"imageCount": 1,
			},
		})
		if err != nil {
			slog.DebugContext(ctx, "failed to start image update activity", "error", err)
			return ""
		}
		return localActivity.ID
	}()
	ctx = s.activityService.Track(ctx, activityID)
	activitylib.AwaitHandlerActivitySlot(ctx, s.activityService, activityID, "0")

	if result, ok := digestPinnedImageUpdateResult(imageRef, activityID, int(time.Since(startTime).Milliseconds())).Get(); ok {
		s.recordDigestPinnedSkip(ctx, activityID, imageRef, result, new(100))
		if logErr := s.eventService.LogResourceEvent(ctx, event.ResourceEvent{
			Type:          event.EventTypeImageScan,
			ResourceType:  "image",
			ResourceName:  imageRef,
			UserID:        types.SystemUser.ID,
			Username:      types.SystemUser.Username,
			EnvironmentID: envcontext.EnvironmentIDOrDefault(ctx),
			Metadata: database.JSON{
				"action":         "check_update",
				"imageRef":       imageRef,
				"hasUpdate":      false,
				"updateType":     UpdateTypeDigest,
				"currentDigest":  result.CurrentDigest,
				"latestDigest":   result.LatestDigest,
				"responseTimeMs": result.ResponseTimeMs,
				"skippedReason":  "digest_pinned",
			},
		}); logErr != nil {
			slog.WarnContext(ctx, "Failed to log digest-pinned image update check event", "imageRef", imageRef, "error", logErr.Error())
		}
		s.completeImageUpdateActivity(ctx, activityID, true, "Image update check skipped")
		return result, nil
	}

	s.appendImageUpdateActivityMessage(ctx, activityID, "Checking "+imageRef, new(20), "Checking remote digest")

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		result := &types.ImageUpdateResponse{
			Error:          "Invalid image reference format",
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}
		s.completeImageUpdateActivity(ctx, activityID, false, result.Error)
		return result, nil
	}

	digestResult, snapshot, err := func() (*types.ImageUpdateResponse, *localImageSnapshot, error) {
		composeBuildRefs, composeErr := s.composeBuildImageRefs(ctx)
		if composeErr != nil {
			return &types.ImageUpdateResponse{Error: composeErr.Error(), CheckTime: time.Now()}, nil, composeErr
		}
		return s.checkSingleImageInBatch(ctx, nil, parts, composeBuildRefs, activityID, startTime)
	}()
	seedDigests := func() map[string]string {
		if err != nil || digestResult == nil || digestResult.UpdateType == UpdateTypeLocal || digestResult.LatestDigest == "" {
			return nil
		}
		return map[string]string{imageRef: digestResult.LatestDigest}
	}()
	containerUpdates, tagErr := s.checkContainerTagUpdates(ctx, kit.Ternary(digestResult.UpdateType == UpdateTypeLocal, nil, []string{imageRef}), nil, seedDigests)
	if tagErr != nil {
		if err == nil {
			if saveErr := s.saveUpdateResultWithSnapshot(ctx, imageRef, digestResult, snapshot); saveErr != nil {
				slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
			}
		}
		s.completeImageUpdateActivity(ctx, activityID, false, tagErr.Error())
		return digestResult, tagErr
	}
	if err != nil {
		result := &types.ImageUpdateResponse{
			Error:          err.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}
		if logErr := s.eventService.LogResourceEvent(ctx, event.ResourceEvent{
			Type:          event.EventTypeImageScan,
			ResourceType:  "image",
			ResourceName:  imageRef,
			UserID:        types.SystemUser.ID,
			Username:      types.SystemUser.Username,
			EnvironmentID: envcontext.EnvironmentIDOrDefault(ctx),
			Metadata: database.JSON{
				"action":    "check_update",
				"imageRef":  imageRef,
				"error":     err.Error(),
				"checkType": "digest",
			},
		}); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check error event", "imageRef", imageRef, "error", logErr.Error())
		}
		if saveErr := s.saveUpdateResultWithSnapshot(ctx, imageRef, result, snapshot); saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
		}
		attached := tags.AttachContainerUpdates(map[string]*types.ImageUpdateResponse{imageRef: result}, containerUpdates)[imageRef]
		s.completeImageUpdateActivity(ctx, activityID, false, attached.Error)
		if attached.HasUpdate {
			// A failed flush is already logged; the records stay pending for the next one.
			_ = s.SendBatchUpdateNotifications(ctx)
			return attached, nil
		}
		return attached, err
	}

	if digestResult.UpdateType == UpdateTypeLocal {
		s.appendImageUpdateActivityMessage(ctx, activityID, imageRef+" — local build, registry check skipped", new(100), "Skipping image update check")
	}
	if logErr := s.eventService.LogResourceEvent(ctx, event.ResourceEvent{
		Type:          event.EventTypeImageScan,
		ResourceType:  "image",
		ResourceName:  imageRef,
		UserID:        types.SystemUser.ID,
		Username:      types.SystemUser.Username,
		EnvironmentID: envcontext.EnvironmentIDOrDefault(ctx),
		Metadata: database.JSON{
			"action":         "check_update",
			"imageRef":       imageRef,
			"hasUpdate":      digestResult.HasUpdate,
			"updateType":     digestResult.UpdateType,
			"currentDigest":  digestResult.CurrentDigest,
			"latestDigest":   digestResult.LatestDigest,
			"responseTimeMs": digestResult.ResponseTimeMs,
		},
	}); logErr != nil {
		slog.WarnContext(ctx, "Failed to log image update check event", "imageRef", imageRef, "error", logErr.Error())
	}
	if saveErr := s.saveUpdateResultWithSnapshot(ctx, imageRef, digestResult, snapshot); saveErr != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
	}

	result := tags.AttachContainerUpdates(map[string]*types.ImageUpdateResponse{imageRef: digestResult}, containerUpdates)[imageRef]
	if digestResult.HasUpdate || len(containerUpdates) > 0 {
		// A failed flush is already logged; the records stay pending for the next one.
		_ = s.SendBatchUpdateNotifications(ctx)
	}

	s.completeImageUpdateActivity(ctx, activityID, true, kit.Ternary(result.HasUpdate, "Image update available", "Image update check completed"))
	return result, nil
}

func digestPinnedImageUpdateResult(imageRef, activityID string, responseTimeMs int) mo.Option[*types.ImageUpdateResponse] {
	pinnedDigest, ok := digest.FromReferenceSuffix(strings.TrimSpace(imageRef))
	if !ok {
		return mo.None[*types.ImageUpdateResponse]()
	}

	tag := func() string {
		if named, err := reference.ParseNormalizedNamed(strings.TrimSpace(imageRef)); err == nil {
			if tagged, localOk := named.(reference.NamedTagged); localOk {
				return tagged.Tag()
			}
		}
		return "latest"
	}()

	return mo.Some(&types.ImageUpdateResponse{
		UpdateType:     UpdateTypeDigest,
		CurrentVersion: tag,
		LatestVersion:  tag,
		CurrentDigest:  pinnedDigest,
		LatestDigest:   pinnedDigest,
		CheckTime:      time.Now(),
		ResponseTimeMs: responseTimeMs,
		ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
	})
}

func isLocalBuildImageRef(imageRef string, composeBuildRefs map[string]struct{}) bool {
	if named, err := reference.ParseNormalizedNamed(strings.TrimSpace(imageRef)); err == nil &&
		slices.Contains([]string{imageref.LocalBuildRegistry, imageref.PodmanLocalRegistry}, kitregistry.Normalize(reference.Domain(named))) {
		return true
	}
	_, ok := composeBuildRefs[refs.NormalizeImageUpdateRef(imageRef)]
	return ok
}

func (s *ImageUpdateService) parseImageReference(imageRef string) *ImageParts {
	if named, err := reference.ParseNormalizedNamed(imageRef); err == nil {
		return &ImageParts{
			Registry:   kitregistry.Normalize(reference.Domain(named)),
			Repository: reference.Path(named),
			Tag: func() string {
				if tagged, ok := named.(reference.NamedTagged); ok {
					return tagged.Tag()
				}
				return "latest"
			}(),
		}
	}

	splitTag := func(repository string) (string, string) {
		repoParts := strings.Split(repository, ":")
		if len(repoParts) == 1 {
			return repository, "latest"
		}
		return repoParts[0], repoParts[1]
	}
	if _, ok := digest.FromReferenceSuffix(imageRef); ok {
		digestParts := strings.Split(imageRef, "@")
		if len(digestParts) != 2 {
			return nil
		}
		parts := strings.Split(digestParts[0], "/")
		if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
			return &ImageParts{
				Registry:   kitregistry.Normalize(parts[0]),
				Repository: strings.Join(parts[1:], "/"),
				Tag:        "latest",
			}
		}
		return &ImageParts{
			Registry:   kitregistry.Normalize("docker.io"),
			Repository: kit.Ternary(len(parts) == 1, "library/"+parts[0], digestParts[0]),
			Tag:        "latest",
		}
	}
	parts := strings.Split(imageRef, "/")
	switch {
	case len(parts) == 1:
		repository, tag := splitTag(parts[0])
		return &ImageParts{
			Registry:   kitregistry.Normalize("docker.io"),
			Repository: "library/" + repository,
			Tag:        tag,
		}
	case strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":"):
		repository, tag := splitTag(strings.Join(parts[1:], "/"))
		return &ImageParts{
			Registry:   kitregistry.Normalize(parts[0]),
			Repository: repository,
			Tag:        tag,
		}
	default:
		repository, tag := splitTag(imageRef)
		return &ImageParts{
			Registry:   kitregistry.Normalize("docker.io"),
			Repository: repository,
			Tag:        tag,
		}
	}
}

func (s *ImageUpdateService) getImageRefByID(ctx context.Context, imageID string) (string, error) {
	imageID = strings.TrimPrefix(imageID, "sha256:")

	if imageRef, ok := func() (string, bool) {
		inspectResponse, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
			return dockerClient.ImageInspect(ctx, imageID)
		})
		if inspectErr != nil {
			return "", false
		}
		// Prefer a real tag over a digest name kept by the containerd image store.
		for _, pinned := range []bool{false, true} {
			for _, tag := range inspectResponse.RepoTags {
				if tag != "<none>:<none>" && refs.IsDigestPinnedReference(tag) == pinned {
					return tag, true
				}
			}
		}
		if repo, found := imageref.RepoDigestRepository(inspectResponse.RepoDigests); found {
			return repo + ":latest", true
		}
		return "", false
	}(); ok {
		return imageRef, nil
	}

	// Pruned images: look up the reference from containers started from this image ID.
	if imageRef, ok := func() (string, bool) {
		containers, listErr := s.dockerService.ListContainers(ctx, false)
		if listErr != nil {
			return "", false
		}
		for _, c := range containers {
			if c.ImageID != "sha256:"+imageID && c.ImageID != imageID {
				continue
			}
			if c.Image != "" && !strings.HasPrefix(c.Image, "sha256:") && !strings.Contains(c.Image, "@sha256:") {
				return c.Image, true
			}
		}
		return "", false
	}(); ok {
		return imageRef, nil
	}

	return "", common.Classify(common.ErrNotFound, fmt.Errorf("image not found: no local image or running container found for %s", imageID))
}

func (s *ImageUpdateService) getAllImageRefs(ctx context.Context, limit int) ([]string, error) {
	images, err := s.dockerService.ListImages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker images: %w", err)
	}

	containers, err := s.dockerService.ListContainers(ctx, false)
	if err != nil {
		slog.WarnContext(ctx, "failed to list Docker containers; continuing without update-check opt-out filtering", "error", err.Error())
	}
	return filterImageSummariesByContainerOptOut(images, containers, limit), nil
}

func newMonitoringEligibility(containers []container.Summary) monitoringEligibility {
	eligibility := monitoringEligibility{
		byImageID:   map[string]bool{},
		byRef:       map[string]bool{},
		byContainer: map[string]bool{},
	}
	for _, summary := range containers {
		eligible := !imageref.IsUpdateCheckDisabled(summary.Labels)
		eligibility.byContainer[summary.ID] = eligible
		if imageID := strings.TrimSpace(summary.ImageID); imageID != "" {
			eligibility.byImageID[imageID] = eligibility.byImageID[imageID] || eligible
		}
		if normalizedRef := refs.NormalizeImageUpdateRef(summary.Image); normalizedRef != "" {
			eligibility.byRef[normalizedRef] = eligibility.byRef[normalizedRef] || eligible
		}
	}
	return eligibility
}

// imageEligible: unused images are eligible; used ones need a consumer that has not opted out.
func (e monitoringEligibility) imageEligible(imageID, normalizedRef string) bool {
	eligibleByID, usedByID := e.byImageID[strings.TrimSpace(imageID)]
	eligibleRef, usedByRef := e.byRef[normalizedRef]
	return (!usedByID && !usedByRef) || eligibleByID || eligibleRef
}

func filterImageSummariesByContainerOptOut(images []image.Summary, containers []container.Summary, limit int) []string {
	eligibility := newMonitoringEligibility(containers)

	seen := make(map[string]struct{})
	filtered := make([]string, 0)

	// Equivalent spellings share one candidate; digest-pinned refs dedupe on their own spelling.
	addCandidate := func(imageRef, imageID string) bool {
		imageRef = strings.TrimSpace(imageRef)
		if imageRef == "" || imageRef == "<none>:<none>" || refs.IsImageIDLikeReference(imageRef) {
			return false
		}
		key := cmp.Or(refs.NormalizeImageUpdateRef(imageRef), imageRef)
		if _, exists := seen[key]; exists {
			return false
		}

		if !eligibility.imageEligible(imageID, key) {
			return false
		}

		seen[key] = struct{}{}
		filtered = append(filtered, imageRef)
		return limit > 0 && len(filtered) >= limit
	}

	for _, summary := range images {
		for _, imageRef := range summary.RepoTags {
			if refs.IsDigestPinnedReference(imageRef) {
				continue
			}
			if addCandidate(imageRef, summary.ID) {
				return filtered
			}
		}
	}

	// Containers may run images whose tag is gone locally.
	for _, summary := range containers {
		if addCandidate(summary.Image, summary.ImageID) {
			return filtered
		}
	}

	return filtered
}

func (s *ImageUpdateService) inspectLocalImageSnapshot(ctx context.Context, imageRef string, composeBuildRefs map[string]struct{}) (*localImageSnapshot, error) {
	inspectResponse, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, imageRef)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}

	var allDigests, repositoryDigests []string
	requested, referenceErr := refs.NormalizeReference(imageRef)

	for _, repoDigest := range inspectResponse.RepoDigests {
		digestValue, ok := digest.FromReferenceSuffix(repoDigest)
		if !ok {
			continue
		}

		allDigests = append(allDigests, digestValue)
		repositoryRef, parseErr := refs.NormalizeReference(repoDigest)
		if referenceErr == nil && parseErr == nil && repositoryRef.RegistryHost == requested.RegistryHost && repositoryRef.Repository == requested.Repository {
			repositoryDigests = append(repositoryDigests, digestValue)
		}
	}

	repo, tag := extractRepoAndTagFromImage(inspectResponse.InspectResponse, s.parseImageReference(imageRef))
	created, createdErr := time.Parse(time.RFC3339Nano, inspectResponse.Created)
	if createdErr != nil {
		slog.DebugContext(ctx, "Local image has no valid created timestamp", "imageRef", imageRef, "error", createdErr)
	}

	if inspectResponse.ID != "" {
		s.releaseLinks.Set(cache.Entry[string, string]{
			Key:   inspectResponse.ID,
			Value: releasenotes.ReleaseLinkFromLabels(kit.FromPtr(inspectResponse.Config).Labels),
		})
	}

	return &localImageSnapshot{
		ImageID:           inspectResponse.ID,
		Repository:        repo,
		Tag:               tag,
		PrimaryDigest:     slices.Concat(repositoryDigests, allDigests, []string{inspectResponse.ID})[0],
		AllDigests:        kit.Ternary(len(allDigests) > 0, allDigests, []string{inspectResponse.ID}),
		RepositoryDigests: repositoryDigests,
		RepoTags:          inspectResponse.RepoTags,
		IsLocalBuild:      imageref.IsLocalImage(inspectResponse.InspectResponse) || isLocalBuildImageRef(imageRef, composeBuildRefs),
		Version:           strings.TrimSpace(kit.FromPtr(inspectResponse.Config).Labels[registryInternal.OCIImageVersionLabel]),
		Created:           created,
		Platform: v1.Platform{
			OS:           inspectResponse.Os,
			Architecture: inspectResponse.Architecture,
			Variant:      inspectResponse.Variant,
		},
	}, nil
}

func (s *ImageUpdateService) CheckImageUpdateByID(ctx context.Context, imageID string) (*types.ImageUpdateResponse, error) {
	imageRef, err := s.getImageRefByID(ctx, imageID)
	if err != nil {
		if logErr := s.eventService.LogResourceEvent(ctx, event.ResourceEvent{
			Type:          event.EventTypeImageScan,
			ResourceType:  "image",
			ResourceID:    imageID,
			UserID:        types.SystemUser.ID,
			Username:      types.SystemUser.Username,
			EnvironmentID: envcontext.EnvironmentIDOrDefault(ctx),
			Metadata: database.JSON{
				"action":  "check_update_by_id",
				"imageID": imageID,
				"error":   err.Error(),
			},
		}); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check by ID error event", "imageId", imageID, "error", logErr.Error())
		}
		return nil, fmt.Errorf("failed to get image reference: %w", err)
	}
	result, err := s.CheckImageUpdate(ctx, imageRef)
	if err != nil {
		return nil, err
	}
	if len(result.ContainerUpdates) == 0 {
		saveErr := func() error {
			dockerImage, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
				return dockerClient.ImageInspect(ctx, imageID)
			})
			if inspectErr != nil {
				return fmt.Errorf("failed to inspect image: %w", inspectErr)
			}
			repo, tag := extractRepoAndTagFromImage(dockerImage.InspectResponse, s.parseImageReference(imageRef))
			return s.savePreparedUpdateResult(ctx, imageID, repo, tag, result)
		}()
		if saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result by ID", "imageId", imageID, "error", saveErr.Error())
		}
	}
	return result, nil
}

func (s *ImageUpdateService) saveUpdateResultWithSnapshot(ctx context.Context, imageRef string, result *types.ImageUpdateResponse, snapshot *localImageSnapshot) error {
	parts := s.parseImageReference(imageRef)
	if parts == nil {
		return errors.New("invalid image reference")
	}
	repository := func() string {
		repositoryPath := strings.TrimSpace(parts.Repository)
		if parts.Registry == "docker.io" && repositoryPath != "" && !strings.Contains(repositoryPath, "/") {
			repositoryPath = "library/" + repositoryPath
		}
		if strings.TrimSpace(parts.Registry) == "" {
			return repositoryPath
		}
		return fmt.Sprintf("%s/%s", strings.TrimSpace(parts.Registry), repositoryPath)
	}()
	syntheticID := fmt.Sprintf("ref::%s@%s", strings.ToLower(strings.TrimSpace(repository)), strings.TrimSpace(parts.Tag))

	local, err := func() (*localImageSnapshot, error) {
		if kit.FromPtr(snapshot).ImageID != "" {
			return snapshot, nil
		}
		inspectResponse, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
			return dockerClient.ImageInspect(ctx, imageRef)
		})
		if inspectErr != nil {
			return nil, inspectErr
		}
		repo, tag := extractRepoAndTagFromImage(inspectResponse.InspectResponse, parts)
		if inspectResponse.ID != "" {
			s.releaseLinks.Set(cache.Entry[string, string]{
				Key:   inspectResponse.ID,
				Value: releasenotes.ReleaseLinkFromLabels(kit.FromPtr(inspectResponse.Config).Labels),
			})
		}
		return &localImageSnapshot{
			ImageID:    inspectResponse.ID,
			Repository: repo,
			Tag:        tag,
			RepoTags:   inspectResponse.RepoTags,
		}, nil
	}()
	if err != nil {
		slog.DebugContext(ctx, "Saving image update result with synthetic ref ID",
			"imageRef", imageRef,
			"error", err.Error(),
			"repository", repository,
			"tag", parts.Tag,
			"syntheticId", syntheticID)
		// Persist registry results under a ref-scoped record so pruned images keep their update status.
		return s.savePreparedUpdateResult(ctx, syntheticID, repository, parts.Tag, result)
	}

	checked := refs.NormalizeImageUpdateRef(imageRef)
	realTags := make([]string, 0, len(local.RepoTags))
	for _, repoTag := range local.RepoTags {
		if !refs.IsDigestPinnedReference(repoTag) {
			realTags = append(realTags, refs.NormalizeImageUpdateRef(repoTag))
		}
	}
	tagID := fmt.Sprintf("tag::%s::%s@%s", local.ImageID, strings.ToLower(strings.TrimSpace(repository)), strings.TrimSpace(parts.Tag))
	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored ImageUpdateRecord
		if findErr := tx.Where("id = ? AND environment_id = ?", local.ImageID, envcontext.EnvironmentIDOrDefault(ctx)).Limit(1).Find(&stored).Error; findErr != nil {
			return findErr
		}
		primaryRepo, primaryTag := local.Repository, local.Tag
		if slices.Contains(realTags, refs.NormalizeImageUpdateRef(stored.Repository+":"+stored.Tag)) {
			primaryRepo, primaryTag = stored.Repository, stored.Tag
		}
		if !slices.Contains(realTags, checked) || refs.NormalizeImageUpdateRef(primaryRepo+":"+primaryTag) == checked {
			if saveErr := savePreparedUpdateResultWithTx(tx, local.ImageID, primaryRepo, primaryTag, result); saveErr != nil {
				return saveErr
			}
			return tx.Where("id = ? AND environment_id = ?", tagID, envcontext.EnvironmentIDOrDefault(ctx)).Delete(&ImageUpdateRecord{}).Error
		}
		if saveErr := savePreparedUpdateResultWithTx(tx, tagID, repository, parts.Tag, result); saveErr != nil {
			return saveErr
		}
		return tx.Model(&ImageUpdateRecord{}).Where("id = ? AND environment_id = ?", tagID, envcontext.EnvironmentIDOrDefault(ctx)).Update("image_id", local.ImageID).Error
	}); transactionErr != nil {
		return transactionErr
	}
	s.publishImageUpdateChange(ctx)
	return nil
}

// extractRepoAndTagFromImage reads the image's first usable tag; an untagged image uses fallback's tag when set.
func extractRepoAndTagFromImage(dockerImage image.InspectResponse, fallback *ImageParts) (repo, tag string) {
	untagged := cmp.Or(strings.TrimSpace(kit.FromPtr(fallback).Tag), "<none>")
	primary := max(slices.IndexFunc(dockerImage.RepoTags, func(repoTag string) bool {
		return repoTag != "<none>:<none>" && !refs.IsDigestPinnedReference(repoTag)
	}), 0)
	if len(dockerImage.RepoTags) > 0 && dockerImage.RepoTags[primary] != "<none>:<none>" {
		if named, err := reference.ParseNormalizedNamed(dockerImage.RepoTags[primary]); err == nil {
			if tagged, ok := named.(reference.NamedTagged); ok {
				return reference.FamiliarName(named), tagged.Tag()
			}
			return reference.FamiliarName(named), "latest"
		}

		parts := strings.SplitN(dockerImage.RepoTags[primary], ":", 2)
		if len(parts) > 1 {
			return parts[0], parts[1]
		}
		return parts[0], "latest"
	}

	if repoCandidate, found := imageref.RepoDigestRepository(dockerImage.RepoDigests); found {
		return repoCandidate, untagged
	}

	return "<none>", untagged
}

func buildImageUpdateRecord(
	environmentID, imageID, repo, tag string,
	result *types.ImageUpdateResponse,
	notificationSent bool,
	latestSeenAt *time.Time,
	carried, blocked *ImageUpdateRecord,
) *ImageUpdateRecord {
	sameCarriedDigest := carried != nil && (result.LatestDigest == "" || kit.FromPtr(carried.LatestDigest) == result.LatestDigest)
	return &ImageUpdateRecord{ //exhaustruct:enforce
		EnvironmentID: environmentID,
		BaseModel: database.BaseModel{
			ID:        "",
			CreatedAt: time.Time{},
			UpdatedAt: nil,
		},
		ProjectID:    "",
		ServiceName:  "",
		PolicyKey:    "",
		ContainerID:  "",
		ImageID:      "",
		CheckTime:    result.CheckTime,
		LatestSeenAt: latestSeenAt,
		LatestVersion: cmp.Or(
			mo.EmptyableToOption(result.LatestVersion).ToPointer(),
			kit.Ternary(sameCarriedDigest, kit.FromPtr(carried).LatestVersion, nil),
		),
		CurrentDigest:    mo.EmptyableToOption(result.CurrentDigest).ToPointer(),
		LatestDigest:     cmp.Or(mo.EmptyableToOption(result.LatestDigest).ToPointer(), kit.FromPtr(carried).LatestDigest),
		CurrentCreated:   cmp.Or(result.CurrentCreated, kit.FromPtr(carried).CurrentCreated),
		LatestCreated:    cmp.Or(result.LatestCreated, kit.Ternary(sameCarriedDigest, kit.FromPtr(carried).LatestCreated, nil)),
		LastError:        mo.EmptyableToOption(result.Error).ToPointer(),
		BlockedReason:    kit.FromPtr(blocked).BlockedReason,
		BlockedPolicy:    kit.FromPtr(blocked).BlockedPolicy,
		AuthMethod:       mo.EmptyableToOption(result.AuthMethod).ToPointer(),
		AuthUsername:     mo.EmptyableToOption(result.AuthUsername).ToPointer(),
		AuthRegistry:     mo.EmptyableToOption(result.AuthRegistry).ToPointer(),
		ID:               imageID,
		Repository:       repo,
		Tag:              tag,
		UpdateType:       result.UpdateType,
		CurrentVersion:   cmp.Or(result.CurrentVersion, kit.FromPtr(carried).CurrentVersion, tag),
		ResponseTimeMs:   result.ResponseTimeMs,
		HasUpdate:        result.HasUpdate || strings.TrimSpace(result.Error) != "" && kit.FromPtr(carried).HasUpdate,
		UsedCredential:   result.UsedCredential,
		NotificationSent: notificationSent,
	}
}

func savePreparedUpdateResultWithTx(tx *gorm.DB, imageID, repo, tag string, result *types.ImageUpdateResponse) error {
	var existingRecord ImageUpdateRecord
	hasExisting := tx.Where("id = ? AND environment_id = ?", imageID, envcontext.EnvironmentIDOrDefault(tx.Statement.Context)).First(&existingRecord).Error == nil

	// A registry rate limit is transient, so keep the previous good result.
	if hasExisting &&
		strings.TrimSpace(result.Error) != "" &&
		registryInternal.IsRateLimitErrorString(result.Error) &&
		strings.TrimSpace(kit.FromPtr(existingRecord.LastError)) == "" {
		slog.DebugContext(tx.Statement.Context, "Preserving previous image update result; check hit a registry rate limit",
			"imageId", imageID, "repository", repo, "tag", tag, "error", result.Error)
		return nil
	}

	sameDigest := hasExisting && result.LatestDigest != "" && kit.FromPtr(existingRecord.LatestDigest) == result.LatestDigest
	sameLatest := kit.FromPtr(existingRecord.LatestDigest) == result.LatestDigest &&
		(result.UpdateType == UpdateTypeDigest || kit.FromPtr(existingRecord.LatestVersion) == result.LatestVersion)
	// A failed check keeps its pending target so transient errors do not restart the auto-update delay.
	failed := !result.HasUpdate && strings.TrimSpace(result.Error) != "" && existingRecord.LatestSeenAt != nil

	// Reset notification_sent only when the update state changed; latest_seen_at keeps the first-seen time.
	record := buildImageUpdateRecord(envcontext.EnvironmentIDOrDefault(tx.Statement.Context), imageID, repo, tag, result,
		existingRecord.NotificationSent &&
			(failed || hasExisting &&
				existingRecord.HasUpdate == result.HasUpdate &&
				(!result.HasUpdate || sameLatest)),
		func() *time.Time {
			switch {
			case failed:
				return existingRecord.LatestSeenAt
			case !result.HasUpdate:
				return nil
			case sameLatest:
				return cmp.Or(existingRecord.LatestSeenAt, new(result.CheckTime))
			}
			return new(result.CheckTime)
		}(),
		kit.Ternary(failed || sameDigest, &existingRecord, nil),
		kit.Ternary(hasExisting && (failed || result.HasUpdate && sameLatest), &existingRecord, nil),
	)
	if err := tx.Save(record).Error; err != nil {
		return err
	}
	*result = types.ImageUpdateResponse{ //exhaustruct:enforce
		ImageUpdate:      result.ImageUpdate,
		ImageRef:         result.ImageRef,
		ContainerUpdates: result.ContainerUpdates,
		CheckTime:        result.CheckTime,
		ActivityID:       result.ActivityID,
		UpdateType:       result.UpdateType,
		CurrentVersion:   result.CurrentVersion,
		LatestVersion:    result.LatestVersion,
		CurrentDigest:    result.CurrentDigest,
		LatestDigest:     result.LatestDigest,
		CurrentCreated:   result.CurrentCreated,
		LatestCreated:    result.LatestCreated,
		Error:            result.Error,
		BlockedReason:    kit.Ternary(result.HasUpdate, kit.FromPtr(record.BlockedReason), ""),
		AuthMethod:       result.AuthMethod,
		AuthUsername:     result.AuthUsername,
		AuthRegistry:     result.AuthRegistry,
		ReleaseURL:       result.ReleaseURL,
		ResponseTimeMs:   result.ResponseTimeMs,
		HasUpdate:        result.HasUpdate,
		Blocked:          result.HasUpdate && kit.FromPtr(record.BlockedReason) != "",
		UsedCredential:   result.UsedCredential,
	}
	return nil
}

func (s *ImageUpdateService) savePreparedUpdateResult(ctx context.Context, imageID, repo, tag string, result *types.ImageUpdateResponse) error {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return savePreparedUpdateResultWithTx(tx, imageID, repo, tag, result)
	}); err != nil {
		return err
	}
	s.publishImageUpdateChange(ctx)
	return nil
}

func (s *ImageUpdateService) MarkImageRefUpToDateAfterPull(ctx context.Context, imageRef string) error {
	snapshot, err := s.inspectLocalImageSnapshot(ctx, imageRef, nil)
	if err != nil {
		return fmt.Errorf("inspect pulled image: %w", err)
	}

	checkTime := time.Now().UTC()
	_, tag, repositoryCandidates, hasLookup := imageref.ParseUpdateLookup(imageRef)
	// projectChecks are the digest previews this pull satisfies; previews without a digest result wait for the next check.
	projectChecks, confirmedDigest, err := func() ([]ImageUpdateRecord, string, error) {
		if len(repositoryCandidates) == 0 || len(snapshot.RepositoryDigests) == 0 {
			return nil, "", nil
		}
		var records []ImageUpdateRecord
		if findErr := s.db.WithContext(ctx).
			Where("project_id <> '' AND update_type = ? AND tag = ? AND repository IN ?", UpdateTypeDigest, tag, slices.Collect(maps.Keys(repositoryCandidates))).
			Where("environment_id = ?", envcontext.EnvironmentIDOrDefault(ctx)).
			Where("last_error IS NULL OR last_error = ''").Find(&records).Error; findErr != nil {
			return nil, "", fmt.Errorf("load project update checks after pull: %w", findErr)
		}
		digestMismatch := func(record ImageUpdateRecord) bool {
			return kit.FromPtr(record.LatestDigest) != "" && !slices.Contains(snapshot.RepositoryDigests, kit.FromPtr(record.LatestDigest))
		}
		if !slices.ContainsFunc(records, digestMismatch) {
			return records, "", nil
		}

		// Confirm a moved tag without the digest cache; a registry failure leaves differing previews untouched.
		registryCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry))
		defer cancel()
		latest, inspectErr := s.registryService.InspectImageDigest(registryCtx, imageRef, nil)
		if inspectErr != nil {
			slog.WarnContext(ctx, "Failed to verify pulled image against project previews", "imageRef", imageRef, "error", inspectErr.Error())
		} else if slices.Contains(snapshot.RepositoryDigests, kit.FromPtr(latest).Digest) {
			return records, latest.Digest, nil
		}
		return slices.DeleteFunc(records, digestMismatch), "", nil
	}()
	if err != nil {
		return err
	}
	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if hasLookup && len(repositoryCandidates) > 0 {
			// Only clear synthetic ref:: records; clearing sha256 records marks old-image containers up to date (#2453).
			if clearStaleUpdatesErr := tx.Model(&ImageUpdateRecord{}).
				Where("id LIKE 'ref::%' AND tag = ? AND repository IN ?", tag, slices.Collect(maps.Keys(repositoryCandidates))).
				Where("environment_id = ?", envcontext.EnvironmentIDOrDefault(ctx)).
				Updates(map[string]any{
					"has_update":     false,
					"latest_seen_at": nil,
					"blocked_reason": nil,
					"blocked_policy": nil,
				}).Error; clearStaleUpdatesErr != nil {
				return fmt.Errorf("clear stale image updates: %w", clearStaleUpdatesErr)
			}
		}
		for _, record := range projectChecks {
			// Record the local digest the preview targeted when the pull carries it.
			pulledDigest := kit.Ternary(
				confirmedDigest == "" && slices.Contains(snapshot.RepositoryDigests, kit.FromPtr(record.LatestDigest)),
				kit.FromPtr(record.LatestDigest),
				cmp.Or(confirmedDigest, snapshot.RepositoryDigests[0]),
			)
			// Preserve any preview replaced while the registry lookup was in flight.
			if refreshProjectChecksErr := tx.Model(&ImageUpdateRecord{}).
				Where(map[string]any{
					"id":             record.ID,
					"check_time":     record.CheckTime,
					"policy_key":     record.PolicyKey,
					"latest_digest":  record.LatestDigest,
					"latest_version": record.LatestVersion,
					"current_digest": record.CurrentDigest,
					"last_error":     record.LastError,
					"has_update":     record.HasUpdate,
				}).
				Updates(map[string]any{
					"has_update":      false,
					"latest_seen_at":  nil,
					"blocked_reason":  nil,
					"blocked_policy":  nil,
					"update_type":     UpdateTypeDigest,
					"current_version": cmp.Or(snapshot.Version, tag),
					"latest_version":  cmp.Or(snapshot.Version, tag),
					"current_digest":  pulledDigest,
					"latest_digest":   pulledDigest,
					"current_created": mo.EmptyableToOption(snapshot.Created).ToPointer(),
					"latest_created":  mo.EmptyableToOption(snapshot.Created).ToPointer(),
					"check_time":      checkTime,
				}).Error; refreshProjectChecksErr != nil {
				return fmt.Errorf("refresh project update checks: %w", refreshProjectChecksErr)
			}
		}

		if savePreparedUpdateResultWithTxErr := savePreparedUpdateResultWithTx(tx, snapshot.ImageID, snapshot.Repository, snapshot.Tag, &types.ImageUpdateResponse{
			UpdateType:     UpdateTypeDigest,
			CurrentVersion: cmp.Or(snapshot.Version, snapshot.Tag),
			LatestVersion:  cmp.Or(snapshot.Version, snapshot.Tag),
			CurrentDigest:  snapshot.PrimaryDigest,
			LatestDigest:   snapshot.PrimaryDigest,
			CurrentCreated: mo.EmptyableToOption(snapshot.Created).ToPointer(),
			LatestCreated:  mo.EmptyableToOption(snapshot.Created).ToPointer(),
			CheckTime:      checkTime,
		}); savePreparedUpdateResultWithTxErr != nil {
			return fmt.Errorf("save pulled image update state: %w", savePreparedUpdateResultWithTxErr)
		}

		return nil
	}); transactionErr != nil {
		return transactionErr
	}
	s.publishImageUpdateChange(ctx)
	return nil
}

func (s *ImageUpdateService) MarkImageRefBlocked(ctx context.Context, imageRef string, block UpdateBlock) error {
	_, tag, repositoryCandidates, ok := imageref.ParseUpdateLookup(imageRef)
	if !ok || len(repositoryCandidates) == 0 {
		return fmt.Errorf("invalid image reference %q", imageRef)
	}
	if err := s.db.WithContext(ctx).Model(&ImageUpdateRecord{}).
		Where("has_update = ? AND repository IN ?", true, slices.Collect(maps.Keys(repositoryCandidates))).
		Where("(update_type = ? AND latest_version = ?) OR (update_type <> ? AND tag = ?)", UpdateTypeTag, tag, UpdateTypeTag, tag).
		Where("latest_digest = ? OR latest_digest IS NULL OR latest_digest = ''", block.Digest).
		Updates(map[string]any{
			"blocked_reason": block.Reason,
			"blocked_policy": block.Policy,
		}).Error; err != nil {
		return fmt.Errorf("mark image updates blocked: %w", err)
	}
	s.publishImageUpdateChange(ctx)
	return nil
}

func (s *ImageUpdateService) StoredUpdateByImageID(ctx context.Context, imageID string) (*ImageUpdateRecord, bool, error) {
	if strings.TrimSpace(imageID) == "" {
		return nil, false, nil
	}

	var record ImageUpdateRecord
	if err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", strings.TrimSpace(imageID), envcontext.EnvironmentIDOrDefault(ctx)).
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get stored image update by image id: %w", err)
	}

	return &record, true, nil
}

func (s *ImageUpdateService) GetUnnotifiedUpdates(ctx context.Context) (map[string]*ImageUpdateRecord, error) {
	var records []ImageUpdateRecord
	if err := s.db.WithContext(ctx).
		Where("has_update = ? AND notification_sent = ? AND project_id = ?", true, false, "").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to get unnotified updates: %w", err)
	}

	result := make(map[string]*ImageUpdateRecord)
	for i := range records {
		result[records[i].ID] = &records[i]
	}
	return result, nil
}

func (s *ImageUpdateService) MarkUpdatesAsNotified(ctx context.Context, records []*ImageUpdateRecord) error {
	if len(records) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, record := range records {
			if err := tx.Model(&ImageUpdateRecord{}).
				Where(map[string]any{
					"id":             record.ID,
					"check_time":     record.CheckTime,
					"latest_digest":  record.LatestDigest,
					"latest_version": record.LatestVersion,
				}).
				Update("notification_sent", true).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// groupImageRefs groups refs naming the same registry/repo:tag and round-robin interleaves them by registry; digest-pinned and invalid refs stay alone.
func (s *ImageUpdateService) groupImageRefs(imageRefs []string) [][]string {
	type bucket struct {
		groups [][]string
	}
	type groupLoc struct {
		bucket *bucket
		index  int
	}
	var buckets []*bucket
	bucketByRegistry := make(map[string]*bucket)
	locByNormalizedRef := make(map[string]groupLoc)

	addGroup := func(registryKey, imageRef string) *bucket {
		b, ok := bucketByRegistry[registryKey]
		if !ok {
			b = &bucket{}
			bucketByRegistry[registryKey] = b
			buckets = append(buckets, b)
		}
		b.groups = append(b.groups, []string{imageRef})
		return b
	}

	total := 0
	for _, imageRef := range imageRefs {
		parts := s.parseImageReference(imageRef)
		if parts == nil || digestPinnedImageUpdateResult(imageRef, "", 0).IsPresent() {
			addGroup("", imageRef)
			total++
			continue
		}
		normalizedRef := strings.ToLower(fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag))
		if loc, exists := locByNormalizedRef[normalizedRef]; exists {
			loc.bucket.groups[loc.index] = append(loc.bucket.groups[loc.index], imageRef)
			continue
		}
		b := addGroup(strings.ToLower(strings.TrimSpace(parts.Registry)), imageRef)
		locByNormalizedRef[normalizedRef] = groupLoc{bucket: b, index: len(b.groups) - 1}
		total++
	}

	if len(buckets) == 0 {
		return nil
	}
	if len(buckets) == 1 {
		return buckets[0].groups
	}

	interleaved := make([][]string, 0, total)
	for round := 0; len(buckets) > 0; round++ {
		remaining := buckets[:0]
		for _, b := range buckets {
			if round < len(b.groups) {
				interleaved = append(interleaved, b.groups[round])
				if round+1 < len(b.groups) {
					remaining = append(remaining, b)
				}
			}
		}
		buckets = remaining
	}
	return interleaved
}

func (s *ImageUpdateService) checkSingleImageInBatch(
	ctx context.Context,
	externalCreds []types.ContainerRegistryCredential,
	parts *ImageParts,
	composeBuildRefs map[string]struct{},
	activityID string,
	start time.Time,
) (*types.ImageUpdateResponse, *localImageSnapshot, error) {
	imageRef := fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag)
	snapshot, ldErr := s.inspectLocalImageSnapshot(ctx, imageRef, composeBuildRefs)
	if ldErr == nil && snapshot.IsLocalBuild {
		return &types.ImageUpdateResponse{
			UpdateType:     UpdateTypeLocal,
			CurrentVersion: snapshot.Tag,
			CurrentDigest:  snapshot.PrimaryDigest,
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}, snapshot, nil
	}
	// A missing compose build image has no registry to check.
	if ldErr != nil && errdefs.IsNotFound(ldErr) && isLocalBuildImageRef(imageRef, composeBuildRefs) {
		return &types.ImageUpdateResponse{
			UpdateType:     UpdateTypeLocal,
			CurrentVersion: parts.Tag,
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}, nil, nil
	}

	if acquireErr := s.registryLimiter.Acquire(ctx, parts.Registry); acquireErr != nil {
		return &types.ImageUpdateResponse{
			Error:          acquireErr.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}, nil, acquireErr
	}
	registryCtx, registryCancel := context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry))
	digestResult, digestErr := s.registryService.InspectImageDigest(registryCtx, imageRef, externalCreds)
	registryCancel()
	s.registryLimiter.Release(parts.Registry)
	if digestErr != nil {
		return &types.ImageUpdateResponse{
			Error:          digestErr.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			AuthMethod:     kit.FromPtr(digestResult).AuthMethod,
			AuthUsername:   kit.FromPtr(digestResult).AuthUsername,
			AuthRegistry:   kit.FromPtr(digestResult).AuthRegistry,
			UsedCredential: kit.FromPtr(digestResult).UsedCredential,
		}, nil, digestErr
	}

	if ldErr != nil {
		snapshot, ldErr = s.inspectLocalImageSnapshot(ctx, imageRef, composeBuildRefs)
		if ldErr != nil {
			if errdefs.IsNotFound(ldErr) {
				// Resolved remotely but never pulled locally, so report not-pulled instead of failure or update.
				return &types.ImageUpdateResponse{
					UpdateType:     UpdateTypeNotPulled,
					LatestDigest:   digestResult.Digest,
					CheckTime:      time.Now(),
					ResponseTimeMs: int(time.Since(start).Milliseconds()),
					ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
					AuthMethod:     digestResult.AuthMethod,
					AuthUsername:   digestResult.AuthUsername,
					AuthRegistry:   digestResult.AuthRegistry,
					UsedCredential: digestResult.UsedCredential,
				}, nil, nil
			}
			return &types.ImageUpdateResponse{
				Error:          ldErr.Error(),
				CheckTime:      time.Now(),
				ResponseTimeMs: int(time.Since(start).Milliseconds()),
				ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
				AuthMethod:     digestResult.AuthMethod,
				AuthUsername:   digestResult.AuthUsername,
				AuthRegistry:   digestResult.AuthRegistry,
				UsedCredential: digestResult.UsedCredential,
			}, nil, ldErr
		}
	}

	// Deliberately index-level: both digests are the tag's index digest, so do not compare platform digests.
	slog.DebugContext(ctx, "digest comparison",
		"imageRef", imageRef,
		"primaryLocalDigest", kit.Ternary(slices.Contains(snapshot.AllDigests, digestResult.Digest), digestResult.Digest, snapshot.PrimaryDigest),
		"allLocalDigests", snapshot.AllDigests,
		"remoteDigest", digestResult.Digest,
		"hasUpdate", !slices.Contains(snapshot.AllDigests, digestResult.Digest))

	latestConfig := func() *v1.ConfigFile {
		if slices.Contains(snapshot.AllDigests, digestResult.Digest) {
			return nil
		}
		if acquireErr := s.registryLimiter.Acquire(ctx, parts.Registry); acquireErr != nil {
			slog.WarnContext(ctx, "Failed to acquire registry slot for image config", "imageRef", imageRef, "digest", digestResult.Digest, "error", acquireErr)
			return nil
		}
		defer s.registryLimiter.Release(parts.Registry)
		configCtx, configCancel := context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry))
		defer configCancel()
		cfg, cfgErr := s.registryService.ImageConfig(configCtx, fmt.Sprintf("%s/%s@%s", parts.Registry, parts.Repository, digestResult.Digest), registryInternal.ImageConfigOptions{
			Platform:    snapshot.Platform,
			Credentials: externalCreds,
		})
		if cfgErr != nil {
			slog.WarnContext(ctx, "Failed to resolve remote image config for update", "imageRef", imageRef, "digest", digestResult.Digest, "error", cfgErr)
		}
		return cfg
	}()

	return &types.ImageUpdateResponse{
		HasUpdate:      !slices.Contains(snapshot.AllDigests, digestResult.Digest),
		UpdateType:     UpdateTypeDigest,
		CurrentVersion: cmp.Or(snapshot.Version, snapshot.Tag),
		LatestVersion:  strings.TrimSpace(kit.FromPtr(latestConfig).Config.Labels[registryInternal.OCIImageVersionLabel]),
		CurrentDigest:  kit.Ternary(slices.Contains(snapshot.AllDigests, digestResult.Digest), digestResult.Digest, snapshot.PrimaryDigest),
		LatestDigest:   digestResult.Digest,
		CurrentCreated: mo.EmptyableToOption(snapshot.Created).ToPointer(),
		LatestCreated:  mo.EmptyableToOption(kit.FromPtr(latestConfig).Created.UTC()).ToPointer(),
		CheckTime:      time.Now(),
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
		ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		AuthMethod:     digestResult.AuthMethod,
		AuthUsername:   digestResult.AuthUsername,
		AuthRegistry:   digestResult.AuthRegistry,
		UsedCredential: digestResult.UsedCredential,
	}, snapshot, nil
}

// resolveBatchCredentials prefers caller credentials, else caches stored ones per scan; a resumed check uses stored ones.
func (s *ImageUpdateService) resolveBatchCredentials(ctx context.Context, t flow.Task, input imageCheckInput) []types.ContainerRegistryCredential {
	if supplied, ok, _ := s.requestCredentials.Get(input.CredentialsKey); ok && input.CredentialsKey != "" {
		return supplied
	}
	credentials, _, err := s.credentials.GetOrLoad(t.ActivityID(), func(string) ([]types.ContainerRegistryCredential, bool, error) {
		registries, loadErr := s.registryService.GetEnabledRegistries(ctx)
		if loadErr != nil {
			return nil, false, loadErr
		}
		loaded := make([]types.ContainerRegistryCredential, 0, len(registries))
		for _, reg := range registries {
			if strings.TrimSpace(reg.URL) == "" || strings.TrimSpace(reg.Username) == "" || reg.Token == "" {
				continue
			}

			token, decryptErr := crypto.Decrypt(reg.Token)
			if decryptErr != nil {
				slog.DebugContext(ctx, "failed to decrypt registry token for batch check", "registryUrl", reg.URL, "error", decryptErr.Error())
				continue
			}
			if strings.TrimSpace(token) == "" {
				continue
			}

			loaded = append(loaded, types.ContainerRegistryCredential{
				URL:      reg.URL,
				Username: reg.Username,
				Token:    strings.TrimSpace(token),
				Enabled:  reg.Enabled,
			})
		}
		return loaded, true, nil
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to load enabled registries for batch check", "error", err.Error())
		return nil
	}
	return credentials
}

func (s *ImageUpdateService) recordDigestPinnedSkip(ctx context.Context, activityID, imageRef string, result *types.ImageUpdateResponse, progress *int) {
	if progress == nil {
		s.queueActivityMessage(ctx, activityID, types.ActivityMessageLevelInfo, imageRef+" — digest pinned, skipped")
	} else {
		s.appendImageUpdateActivityMessage(ctx, activityID, imageRef+" — digest pinned, skipped", progress, "Skipping image update check")
	}
	if err := s.saveUpdateResultWithSnapshot(ctx, imageRef, result, nil); err != nil {
		slog.WarnContext(ctx, "Failed to save digest-pinned update result", "imageRef", imageRef, "error", err.Error())
	}
}

// RegisterWorkflows defines the image-check workflow; call it before the host starts.
func (s *ImageUpdateService) RegisterWorkflows(engine *flow.Engine) error {
	checkWorkflow, err := engine.Define(flow.Definition{
		Name:        "image-check",
		Version:     4,
		Fingerprint: "abcd647b2da905b6969c610b7156d1d2778f1a05b9ceabdc0f188aef36c67363",
		Concurrency: imageCheckConcurrency,
		Timeout:     timeouts.DefaultImageUpdateScan,
		Activity: activity.StartActivityRequest{
			Type:          types.ActivityTypeImageUpdateCheck,
			Queue:         true,
			ResourceType:  new("images"),
			Step:          "Checking image updates",
			LatestMessage: "Image update check started",
		},
		Labels: map[string]string{
			"prepare":  "Preparing image update check",
			"discover": "Discovering images",
			"check":    "Checking images",
			"tags":     "Checking tag policies",
			"finalize": "Finishing image update check",
		},
		Steps: []workflow.StepSpec{
			workflow.Step("prepare", engine.Handler(s.prepareImageCheck)),
			workflow.Step("discover", engine.Handler(s.discoverImages)),
			workflow.ForEach("check", engine.Handler(s.checkImage),
				workflow.WithItemsFrom("discover"),
				workflow.WithInputFrom("prepare"),
				workflow.WithMaxParallel(imageCheckConcurrency),
				workflow.WithFailurePolicy(workflow.TolerateFailures),
				workflow.WithMaxAttempts(imageCheckAttempts),
				workflow.WithRetryBackoff(imageCheckRetryBackoff, imageCheckRetryBackoffMax),
				workflow.WithAttemptTimeout(2*time.Minute)),
			workflow.Step("tags", engine.Handler(s.checkTagPolicies), workflow.WithInputFrom("discover")),
			workflow.Step("finalize", engine.Handler(s.finalizeImageCheck), workflow.WithInputFrom("discover", "check")),
		},
		// A reported check returns every ref's result, which outgrows the default output limit on large hosts.
		Options: []workflow.Option{workflow.WithMaxOutputSize(4 << 20), workflow.WithMaxJournalSize(16 << 20)},
	})
	if err != nil {
		return err
	}
	s.engine = engine
	s.checkWorkflow = checkWorkflow
	return nil
}

func (s *ImageUpdateService) CheckWorkflow() *flow.Workflow { return s.checkWorkflow }

func (s *ImageUpdateService) RunImageCheck(ctx context.Context, request types.ImageUpdateCheckRequest) (types.SchedulerOutcome, error) {
	return s.engine.Run(ctx, s.checkWorkflow, request, activitylib.StartRequest{}, nil)
}

// CheckImages returns each ref's result; usable credentials replace the stored registries for this check.
func (s *ImageUpdateService) CheckImages(ctx context.Context, request types.ImageUpdateCheckRequest, credentials []types.ContainerRegistryCredential) (types.ImageUpdateBatchResponse, error) {
	if !request.All && len(request.ImageRefs) == 0 {
		return types.ImageUpdateBatchResponse{}, nil
	}
	credentials = slices.DeleteFunc(slices.Clone(credentials), func(credential types.ContainerRegistryCredential) bool {
		return !credential.Enabled || strings.TrimSpace(credential.URL) == "" || strings.TrimSpace(credential.Username) == "" || strings.TrimSpace(credential.Token) == ""
	})
	input := imageCheckInput{
		ImageUpdateCheckRequest: request,
		Report:                  true,
		CredentialsKey:          kit.Ternary(len(credentials) > 0, uuid.NewV7().String(), ""),
	}
	if input.CredentialsKey != "" {
		s.requestCredentials.Set(cache.Entry[string, []types.ContainerRegistryCredential]{
			Key:   input.CredentialsKey,
			Value: credentials,
		})
	}
	var output imageCheckOutput
	outcome, err := s.engine.Run(ctx, s.checkWorkflow, input, activitylib.StartRequest{}, &output)
	if err != nil {
		// The workflow outlives a caller that stopped waiting, so finalize releases the credentials.
		return nil, err
	}
	s.requestCredentials.Delete(input.CredentialsKey)
	if outcome.Status == types.RunStatusFailed || outcome.Status == types.RunStatusCanceled {
		return nil, errors.New(cmp.Or(outcome.Message, "image update check failed"))
	}
	failures := map[string]string{}
	for _, target := range outcome.Targets {
		if target.ResourceType == "image" {
			failures[target.ID] = target.Message
		}
	}
	results := make(types.ImageUpdateBatchResponse, len(output.Results)+len(failures))
	for _, imageRef := range slices.Concat(slices.Collect(maps.Keys(output.Results)), slices.Collect(maps.Keys(failures))) {
		result := cmp.Or(output.Results[imageRef], &types.ImageUpdateResponse{CheckTime: time.Now()})
		message, failed := failures[imageRef]
		results[imageRef] = &types.ImageUpdateResponse{ //exhaustruct:enforce
			ImageUpdate:      result.ImageUpdate,
			ImageRef:         result.ImageRef,
			ContainerUpdates: result.ContainerUpdates,
			CheckTime:        result.CheckTime,
			ActivityID:       mo.EmptyableToOption(outcome.ActivityID).ToPointer(),
			UpdateType:       result.UpdateType,
			CurrentVersion:   result.CurrentVersion,
			LatestVersion:    result.LatestVersion,
			CurrentDigest:    result.CurrentDigest,
			LatestDigest:     result.LatestDigest,
			CurrentCreated:   result.CurrentCreated,
			LatestCreated:    result.LatestCreated,
			Error:            kit.Ternary(failed, message, result.Error),
			BlockedReason:    result.BlockedReason,
			AuthMethod:       result.AuthMethod,
			AuthUsername:     result.AuthUsername,
			AuthRegistry:     result.AuthRegistry,
			ResponseTimeMs:   result.ResponseTimeMs,
			HasUpdate:        result.HasUpdate,
			Blocked:          result.Blocked,
			UsedCredential:   result.UsedCredential,
			ReleaseURL:       result.ReleaseURL,
		}
	}
	// Failed images get their entries first, so their containers' tag results still attach.
	return tags.AttachContainerUpdates(results, output.ContainerUpdates), nil
}

// MonitoredImageRefs keeps the refs whose containers still permit update checks.
func (s *ImageUpdateService) MonitoredImageRefs(ctx context.Context, imageRefs []string) ([]string, error) {
	listed, err := s.dockerService.ListContainers(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list containers for update-check eligibility: %w", err)
	}
	eligibility := newMonitoringEligibility(listed)
	return slices.DeleteFunc(slices.Clone(imageRefs), func(imageRef string) bool {
		return !eligibility.imageEligible("", refs.NormalizeImageUpdateRef(imageRef))
	}), nil
}

// prepareImageCheck loads the compose build refs every check task needs, once per scan.
func (s *ImageUpdateService) prepareImageCheck(ctx context.Context, t flow.Task) (any, error) {
	buildRefs, err := s.composeBuildImageRefs(ctx)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	if t.ActivityID() != "" {
		s.composeBuildRefs.Set(cache.Entry[string, map[string]struct{}]{
			Key:   t.ActivityID(),
			Value: buildRefs,
		})
	}
	return slices.Sorted(maps.Keys(buildRefs)), nil
}

func (s *ImageUpdateService) resolveComposeBuildRefs(t flow.Task) (map[string]struct{}, error) {
	load := func(string) (map[string]struct{}, bool, error) {
		var buildRefs []string
		if err := t.DecodeOutput("prepare", &buildRefs); err != nil {
			return nil, false, err
		}
		resolved := make(map[string]struct{}, len(buildRefs))
		for _, buildRef := range buildRefs {
			resolved[buildRef] = struct{}{}
		}
		return resolved, true, nil
	}
	if t.ActivityID() == "" {
		resolved, _, err := load("")
		return resolved, err
	}
	resolved, _, err := s.composeBuildRefs.GetOrLoad(t.ActivityID(), load)
	return resolved, err
}

func (s *ImageUpdateService) discoverImages(ctx context.Context, t flow.Task) (any, error) {
	var request imageCheckInput
	if err := t.Payload(&request); err != nil {
		return nil, err
	}
	imageRefs, err := func() ([]string, error) {
		if !request.All {
			return request.ImageRefs, nil
		}
		return s.getAllImageRefs(ctx, 0)
	}()
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("list image references: %w", err))
	}
	s.appendImageUpdateActivityMessage(ctx, t.ActivityID(), fmt.Sprintf("Checking %d image references", len(imageRefs)), nil, "")
	return s.groupImageRefs(imageRefs), nil
}

// checkImage records retryable failures only on the last attempt so a good result is never replaced.
func (s *ImageUpdateService) checkImage(ctx context.Context, t flow.Task) (any, error) {
	var group []string
	if err := t.DecodeItem(&group); err != nil || len(group) == 0 {
		return nil, fmt.Errorf("decode image group: %w", cmp.Or(err, errors.New("empty group")))
	}
	var input imageCheckInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	// A manual check answers its caller, who is gone after a restart; its requester was not rechecked here either.
	if input.Report && t.Recovered() {
		return nil, errCheckInterrupted
	}
	saveCtx := context.WithValue(ctx, batchChangeCoalesceKey{}, true)
	if result, ok := digestPinnedImageUpdateResult(group[0], t.ActivityID(), 0).Get(); ok {
		s.recordDigestPinnedSkip(saveCtx, t.ActivityID(), group[0], result, nil)
		return imageCheckResult{Result: kit.Ternary(input.Report, result, nil)}, nil
	}
	parts := s.parseImageReference(group[0])
	if parts == nil {
		err := common.Classify(common.ErrValidation, errors.New("invalid image reference format"))
		s.queueActivityMessage(ctx, t.ActivityID(), types.ActivityMessageLevelError, group[0]+": "+err.Error())
		return nil, err
	}
	composeBuildRefs, err := s.resolveComposeBuildRefs(t)
	if err != nil {
		return nil, err
	}
	res, snapshot, checkErr := s.checkSingleImageInBatch(ctx, s.resolveBatchCredentials(ctx, t, input), parts, composeBuildRefs, t.ActivityID(), time.Now())
	// Shutdown hands the task back for redelivery rather than recording it as a failed check.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A reported check answers a waiting caller, so it gets one attempt as the batch endpoint always has.
	if checkErr != nil && flow.Retryable(checkErr) && !input.Report && t.Attempt() < imageCheckAttempts {
		return nil, checkErr
	}
	// Errors log as ERROR, updates as SUCCESS, and up-to-date images as INFO.
	level, message := func() (types.ActivityMessageLevel, string) {
		switch {
		case strings.TrimSpace(res.Error) != "":
			return types.ActivityMessageLevelError, fmt.Sprintf("%s: %s", group[0], strings.TrimSpace(res.Error))
		case res.UpdateType == UpdateTypeLocal:
			return types.ActivityMessageLevelInfo, group[0] + " — local build, registry check skipped"
		case res.UpdateType == UpdateTypeNotPulled:
			return types.ActivityMessageLevelInfo, group[0] + " — not pulled locally, no local digest to compare"
		case res.HasUpdate:
			return types.ActivityMessageLevelSuccess, group[0] + " — update available"
		default:
			return types.ActivityMessageLevelInfo, group[0] + " — up to date"
		}
	}()
	s.queueActivityMessage(ctx, t.ActivityID(), level, message)
	if saveErr := s.saveUpdateResultWithSnapshot(saveCtx, group[0], res, snapshot); saveErr != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", group[0], "error", saveErr.Error())
	}
	if checkErr != nil && input.Report {
		// The waiting caller gets the failed response, auth details included, alongside its error.
		return imageCheckResult{Result: res, Failure: checkErr.Error()}, nil
	}
	if checkErr != nil {
		return nil, checkErr
	}
	return imageCheckResult{
		Local:  res.UpdateType == UpdateTypeLocal,
		Digest: kit.Ternary(res.UpdateType != UpdateTypeLocal, res.LatestDigest, ""),
		Result: kit.Ternary(input.Report, res, nil),
	}, nil
}

func (s *ImageUpdateService) checkTagPolicies(ctx context.Context, t flow.Task) (any, error) {
	s.flushActivityMessages(ctx, t.ActivityID())
	groups, results, err := imageCheckResults(t)
	if err != nil {
		return nil, err
	}
	var tagRefs []string
	seedDigests := map[string]string{}
	for index, group := range groups {
		if results[index].Value.Local {
			continue
		}
		tagRefs = append(tagRefs, group...)
		resolvedDigest := cmp.Or(results[index].Value.Digest, kit.FromPtr(results[index].Value.Result).LatestDigest)
		if len(group) > 0 && resolvedDigest != "" && results[index].Err == "" {
			seedDigests[group[0]] = resolvedDigest
		}
	}
	var input imageCheckInput
	if payloadErr := t.Payload(&input); payloadErr != nil {
		return nil, payloadErr
	}
	if input.Report && t.Recovered() {
		return tagCheckOutput{Failures: []types.TargetOutcome{}}, nil
	}
	containerUpdates, err := s.checkContainerTagUpdates(context.WithValue(ctx, batchChangeCoalesceKey{}, true), tagRefs, s.resolveBatchCredentials(ctx, t, input), seedDigests)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	failures := []types.TargetOutcome{}
	for containerID, update := range containerUpdates {
		if update.Error != "" {
			failures = append(failures, types.TargetOutcome{
				ResourceType: "container",
				ID:           containerID,
				Status:       types.RunStatusFailed,
				Message:      update.Error,
			})
		}
	}
	return tagCheckOutput{
		Failures: failures,
		Updates:  kit.Ternary(input.Report, containerUpdates, nil),
	}, nil
}

func (s *ImageUpdateService) finalizeImageCheck(ctx context.Context, t flow.Task) (any, error) {
	s.flushActivityMessages(ctx, t.ActivityID())
	var request imageCheckInput
	if err := t.Payload(&request); err != nil {
		return nil, err
	}
	// Every check task has run, so the caller's credentials are no longer needed.
	s.requestCredentials.Delete(request.CredentialsKey)
	if t.ActivityID() != "" {
		s.composeBuildRefs.Delete(t.ActivityID())
		s.credentials.Delete(t.ActivityID())
	}
	groups, results, err := imageCheckResults(t)
	if err != nil {
		return nil, err
	}
	var tagged tagCheckOutput
	if decodeErr := t.DecodeOutput("tags", &tagged); decodeErr != nil {
		return nil, decodeErr
	}
	checked, failed := 0, 0
	var targets []types.TargetOutcome
	for index, group := range groups {
		checked += len(group)
		if results[index].Err == "" {
			continue
		}
		failed += len(group)
		for _, imageRef := range group {
			targets = append(targets, types.TargetOutcome{
				ResourceType: "image",
				ID:           imageRef,
				Status:       types.RunStatusFailed,
				Message:      results[index].Err,
			})
		}
	}
	targets = append(targets, tagged.Failures...)
	if request.All && (!request.Report || !t.Recovered()) {
		if cleanupErr := s.CleanupOrphanedRecords(ctx); cleanupErr != nil {
			slog.WarnContext(ctx, "failed to cleanup orphaned image update records after check-all", "error", cleanupErr.Error())
		}
	}
	s.changePending.Store(false)
	s.eventService.PublishChange("0", "image_update")
	s.notifyStopMu.RLock()
	if s.notifyStopped {
		s.notifyStopMu.RUnlock()
		_ = s.SendBatchUpdateNotifications(ctx)
	} else {
		notifyCtx := context.WithoutCancel(ctx)
		s.notifyWG.Go(func() {
			defer concurrency.RecoverToError(nil, "image update notification dispatch")
			_ = s.SendBatchUpdateNotifications(notifyCtx)
		})
		s.notifyStopMu.RUnlock()
	}
	outcome := types.SchedulerOutcome{
		Status: kit.Ternary(len(targets) > 0, types.RunStatusPartial, types.RunStatusSucceeded),
		Message: fmt.Sprintf("Image update check completed: %d checked, %d errors", checked, failed) +
			kit.Ternary(len(tagged.Failures) > 0, fmt.Sprintf(", %d tag policy errors", len(tagged.Failures)), ""),
		Targets: targets,
	}
	slog.InfoContext(ctx, "Image update check completed", "totalImages", checked, "failedImages", failed, "failedContainers", len(tagged.Failures))
	if !request.Report {
		return outcome, nil
	}
	reported := types.ImageUpdateBatchResponse{}
	for index, group := range groups {
		if result := results[index].Value.Result; result != nil {
			for _, imageRef := range group {
				reported[imageRef] = new(*result)
			}
		}
	}
	return imageCheckOutput{
		SchedulerOutcome: outcome,
		Results:          reported,
		ContainerUpdates: tagged.Updates,
	}, nil
}

func imageCheckResults(t flow.Task) ([][]string, []flow.Result[imageCheckResult], error) {
	var groups [][]string
	if err := t.DecodeOutput("discover", &groups); err != nil {
		return nil, nil, err
	}
	results, err := flow.Results[imageCheckResult](t, "check")
	if err != nil {
		return nil, nil, err
	}
	if len(results) != len(groups) {
		return nil, nil, fmt.Errorf("image check produced %d results for %d groups", len(results), len(groups))
	}
	merged := make([]flow.Result[imageCheckResult], 0, len(results))
	for _, result := range results {
		merged = append(merged, flow.Result[imageCheckResult]{ //exhaustruct:enforce
			Value: result.Value,
			Err:   cmp.Or(result.Err, result.Value.Failure),
		})
	}
	return groups, merged, nil
}

// SendBatchUpdateNotifications errors only when the pending set is unknown; failed deliveries retry on the next flush.
func (s *ImageUpdateService) SendBatchUpdateNotifications(ctx context.Context) error {
	// Serialize query, send, and mark so concurrent flushes cannot double-send.
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()

	// Detach from the activity's cancellation; ActivityRuntimeContext would be a no-op for app-lifecycle contexts.
	ctx = context.WithoutCancel(ctx)

	notifCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	unnotifiedUpdates, err := func() (map[string]*ImageUpdateRecord, error) {
		records, recordsErr := s.GetUnnotifiedUpdates(notifCtx)
		if recordsErr != nil || len(records) == 0 {
			return records, recordsErr
		}
		// Opted-out or vanished resources stay unnotified until monitoring returns or orphan cleanup removes them.
		listed, listErr := s.dockerService.ListContainers(notifCtx, false)
		if listErr != nil {
			return records, fmt.Errorf("list containers for update-check eligibility: %w", listErr)
		}
		eligibility := newMonitoringEligibility(listed)
		maps.DeleteFunc(records, func(id string, record *ImageUpdateRecord) bool {
			if kit.Ternary(record.ContainerID != "",
				eligibility.byContainer[record.ContainerID],
				eligibility.imageEligible(record.ID, refs.NormalizeImageUpdateRef(record.Repository+":"+record.Tag))) {
				return false
			}
			slog.DebugContext(notifCtx, "Skipping update notification for resource opted out of update checks", "recordId", id)
			return true
		})
		return records, nil
	}()
	switch {
	case err != nil:
		slog.WarnContext(ctx, "Failed to get unnotified updates", "error", err.Error())
		return fmt.Errorf("flush pending update notifications: %w", err)
	case len(unnotifiedUpdates) > 0:
		updatesToNotify := make(map[string]*types.ImageUpdateResponse, len(unnotifiedUpdates))
		linkCtx, cancelLinks := context.WithTimeout(notifCtx, 5*time.Second)
		defer cancelLinks()
		links := s.resolveReleaseLinks(linkCtx, unnotifiedUpdates)

		for _, record := range unnotifiedUpdates {
			ref := record.Repository + ":" + record.Tag
			updatesToNotify[ref+kit.Ternary(record.ContainerID != "", " (container "+record.ContainerID+")", "")] = &types.ImageUpdateResponse{
				HasUpdate:      record.HasUpdate,
				UpdateType:     record.UpdateType,
				CurrentVersion: record.CurrentVersion,
				LatestVersion:  kit.FromPtr(record.LatestVersion),
				CurrentDigest:  kit.FromPtr(record.CurrentDigest),
				LatestDigest:   kit.FromPtr(record.LatestDigest),
				CurrentCreated: record.CurrentCreated,
				LatestCreated:  record.LatestCreated,
				CheckTime:      record.CheckTime,
				ResponseTimeMs: record.ResponseTimeMs,
				Error:          kit.FromPtr(record.LastError),
				AuthMethod:     kit.FromPtr(record.AuthMethod),
				AuthUsername:   kit.FromPtr(record.AuthUsername),
				AuthRegistry:   kit.FromPtr(record.AuthRegistry),
				UsedCredential: record.UsedCredential,
				ReleaseURL:     links[ref],
			}
		}

		slog.InfoContext(ctx, "Sending notifications for unnotified updates", "count", len(updatesToNotify))

		delivered, notifErr := s.notificationService.SendBatchImageUpdateNotification(notifCtx, updatesToNotify)
		if notifErr != nil {
			slog.WarnContext(ctx, "Failed to send batch update notification", "error", notifErr.Error())
		}
		// Mark notified once any provider delivered so healthy providers do not re-send.
		if delivered == 0 {
			slog.DebugContext(ctx, "No providers delivered image update notifications; leaving records unnotified", "count", len(unnotifiedUpdates))
			return nil
		}
		if markErr := s.MarkUpdatesAsNotified(notifCtx, slices.Collect(maps.Values(unnotifiedUpdates))); markErr != nil {
			slog.WarnContext(ctx, "Failed to mark updates as notified", "error", markErr.Error())
		}
	default:
		slog.DebugContext(ctx, "No new updates to notify")
	}
	return nil
}

func (s *ImageUpdateService) resolveReleaseLinks(ctx context.Context, records map[string]*ImageUpdateRecord) map[string]string {
	links := make(map[string]string, len(records))
	var missingRefs []string
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		ref := record.Repository + ":" + record.Tag
		if _, resolved := links[ref]; resolved {
			continue
		}
		imageID := cmp.Or(record.ImageID, kit.Ternary(strings.HasPrefix(record.ID, "sha256:"), record.ID, ""))
		if imageID != "" {
			if cached, ok, _ := s.releaseLinks.Get(imageID); ok {
				links[ref] = cached
				continue
			}
		}
		if _, queued := seen[ref]; !queued {
			seen[ref] = struct{}{}
			missingRefs = append(missingRefs, ref)
		}
	}
	missingRefs = slices.DeleteFunc(missingRefs, func(ref string) bool {
		_, resolved := links[ref]
		return resolved
	})
	if len(missingRefs) == 0 {
		return links
	}

	resolved := make([]string, len(missingRefs))
	var g errgroup.Group
	g.SetLimit(releaseLinkConcurrency)
	for i, ref := range missingRefs {
		g.Go(func() error {
			resolved[i] = s.releaseNotes.ReleaseLink(ctx, ref)
			return nil
		})
	}
	_ = g.Wait()
	for i, ref := range missingRefs {
		links[ref] = resolved[i]
	}
	return links
}

// DeleteRecordsForImages removes rows keyed by id or image_id for the given images.
func (s *ImageUpdateService) DeleteRecordsForImages(ctx context.Context, imageIDs []string) error {
	if len(imageIDs) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).
		Where("id IN ? OR image_id IN ?", imageIDs, imageIDs).
		Where("environment_id = ?", envcontext.EnvironmentIDOrDefault(ctx)).
		Delete(&ImageUpdateRecord{}).Error; err != nil {
		return fmt.Errorf("failed to delete image update records: %w", err)
	}
	return nil
}

func (s *ImageUpdateService) CleanupOrphanedRecords(ctx context.Context) error {
	dockerImages, err := s.dockerService.ListImages(ctx)
	if err != nil {
		return err
	}
	dockerImageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		dockerImageIDs = append(dockerImageIDs, img.ID)
	}

	imageScoped := s.db.WithContext(ctx).
		Where("environment_id = ?", envcontext.EnvironmentIDOrDefault(ctx)).
		Where("container_id = ? AND project_id = ? AND id NOT LIKE 'ref::%'", "", "")
	if len(dockerImageIDs) > 0 {
		imageScoped = imageScoped.Where("id NOT IN ? AND image_id NOT IN ?", dockerImageIDs, dockerImageIDs)
	}
	imageResult := imageScoped.Delete(&ImageUpdateRecord{})
	if imageResult.Error != nil {
		return fmt.Errorf("failed to delete orphaned records: %w", imageResult.Error)
	}

	containers, err := s.dockerService.ListContainers(ctx, false)
	if err != nil {
		return err
	}
	containerIDs := make([]string, 0, len(containers))
	for _, cnt := range containers {
		containerIDs = append(containerIDs, cnt.ID)
	}
	containerScoped := s.db.WithContext(ctx).
		Where("environment_id = ?", envcontext.EnvironmentIDOrDefault(ctx)).
		Where("container_id <> ?", "")
	if len(containerIDs) > 0 {
		containerScoped = containerScoped.Where("container_id NOT IN ?", containerIDs)
	}
	containerResult := containerScoped.Delete(&ImageUpdateRecord{})
	if containerResult.Error != nil {
		return fmt.Errorf("failed to delete orphaned container records: %w", containerResult.Error)
	}

	if deleted := imageResult.RowsAffected + containerResult.RowsAffected; deleted > 0 {
		slog.InfoContext(ctx, "Cleaned up orphaned image update records", "deletedCount", deleted)
	} else {
		slog.InfoContext(ctx, "No orphaned image update records found")
	}
	return nil
}

func (s *ImageUpdateService) AggregateCheckImages(ctx context.Context) (types.ApiResponse[types.ImageUpdateBatchResponse], error) {
	if scope, ok := httpx.EnvironmentScopeFromContext(ctx); ok && scope.Timeout == 0 {
		ctx = httpx.WithEnvironmentScope(ctx, &httpx.EnvironmentScope{ //exhaustruct:enforce
			Handler: scope.Handler,
			Request: scope.Request,
			Resolve: scope.Resolve,
			Path:    scope.Path,
			IDs:     scope.IDs,
			Body:    scope.Body,
			Timeout: timeouts.DefaultImageUpdateScan,
		})
	}
	return httpx.CollectEnvironmentData(ctx, nil, func(environments []types.EnvironmentData[types.ImageUpdateBatchResponse]) types.ImageUpdateBatchResponse {
		results := types.ImageUpdateBatchResponse{}
		for _, environment := range environments {
			maps.Copy(results, environment.Data)
		}
		return results
	})
}

func (s *ImageUpdateService) GetUpdateSummary(ctx context.Context) (*types.ImageUpdateSummary, error) {
	dockerImages, err := s.dockerService.ListImages(ctx)
	if err != nil {
		return nil, err
	}

	liveImageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		liveImageIDs = append(liveImageIDs, img.ID)
	}

	return s.getUpdateSummaryForImageIDs(ctx, liveImageIDs)
}

func (s *ImageUpdateService) getUpdateSummaryForImageIDs(ctx context.Context, imageIDs []string) (*types.ImageUpdateSummary, error) {
	if len(imageIDs) == 0 {
		return &types.ImageUpdateSummary{}, nil
	}

	var aggregate struct {
		ImagesWithUpdates int64 `gorm:"column:images_with_updates"`
		DigestUpdates     int64 `gorm:"column:digest_updates"`
		ErrorsCount       int64 `gorm:"column:errors_count"`
	}
	if err := s.db.WithContext(ctx).
		Model(&ImageUpdateRecord{}).
		Select(`
			COUNT(DISTINCT CASE WHEN has_update THEN COALESCE(NULLIF(image_id, ''),id) END) AS images_with_updates,
			COUNT(DISTINCT CASE WHEN has_update AND update_type = ? THEN COALESCE(NULLIF(image_id, ''),id) END) AS digest_updates,
			COUNT(DISTINCT CASE WHEN last_error IS NOT NULL AND last_error != '' THEN COALESCE(NULLIF(image_id, ''),id) END) AS errors_count
		`, "digest").
		Where("id IN ? OR image_id IN ?", imageIDs, imageIDs).
		Where("environment_id = ?", envcontext.EnvironmentIDOrDefault(ctx)).
		Scan(&aggregate).Error; err != nil {
		return nil, err
	}

	return &types.ImageUpdateSummary{
		TotalImages:       len(imageIDs),
		ImagesWithUpdates: int(aggregate.ImagesWithUpdates),
		DigestUpdates:     int(aggregate.DigestUpdates),
		ErrorsCount:       int(aggregate.ErrorsCount),
	}, nil
}

func (s *ImageUpdateService) checkContainerTagUpdates(
	ctx context.Context,
	imageRefs []string,
	credentials []types.ContainerRegistryCredential,
	seedDigests map[string]string,
) (map[string]*types.ImageUpdateResponse, error) {
	wanted := map[string]bool{}
	for _, imageRef := range imageRefs {
		if normalized := refs.NormalizeImageUpdateRef(imageRef); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return map[string]*types.ImageUpdateResponse{}, nil
	}
	listed, err := s.dockerService.ListContainers(ctx, false)
	if err != nil {
		return map[string]*types.ImageUpdateResponse{}, fmt.Errorf("list containers for tag update checks: %w", err)
	}
	return s.tags.Check(ctx, tags.CheckRequest{
		Listed:      listed,
		Wanted:      wanted,
		Credentials: credentials,
		SeedDigests: seedDigests,
		ClearDigestTracked: func(deleteCtx context.Context, containerIDs []string) error {
			// Clear the stored results of containers whose policy no longer tracks tags, in bounded IN-list chunks.
			for chunk := range slices.Chunk(containerIDs, staleDigestRecordDeleteChunk) {
				if deleteErr := s.db.WithContext(deleteCtx).Where("container_id IN ?", chunk).Delete(&ImageUpdateRecord{}).Error; deleteErr != nil {
					return deleteErr
				}
			}
			return nil
		},
		Save: s.saveContainerTagResult,
	})
}

func (s *ImageUpdateService) saveContainerTagResult(ctx context.Context, cnt container.Summary, result *types.ImageUpdateResponse) error {
	parsed, err := refs.NormalizeReference(cnt.Image)
	if err != nil {
		return err
	}
	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A previous good result must not survive a rate limit under a different policy.
		if deleteStaleUpdatesErr := tx.Where("id = ? AND policy_key <> ?", "container::"+cnt.ID, imageref.UpdatePolicyKey(cnt.Image, cnt.Labels)).
			Delete(&ImageUpdateRecord{}).Error; deleteStaleUpdatesErr != nil {
			return deleteStaleUpdatesErr
		}
		if savePreparedUpdateResultWithTxErr := savePreparedUpdateResultWithTx(
			tx,
			"container::"+cnt.ID,
			parsed.RegistryHost+"/"+parsed.Repository,
			parsed.Tag,
			result,
		); savePreparedUpdateResultWithTxErr != nil {
			return savePreparedUpdateResultWithTxErr
		}
		return tx.Model(&ImageUpdateRecord{}).Where("id = ?", "container::"+cnt.ID).Updates(map[string]any{
			"container_id": cnt.ID,
			"image_id":     cnt.ImageID,
			"policy_key":   imageref.UpdatePolicyKey(cnt.Image, cnt.Labels),
		}).Error
	}); transactionErr != nil {
		return transactionErr
	}
	s.publishImageUpdateChange(ctx)
	return nil
}
