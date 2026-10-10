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
	"time"
	"uuid"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"go.getarcane.app/updater/digest"
	"go.getarcane.app/updater/refs"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate/children/tags"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ratelimit"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

const (
	imageCheckConcurrency = 10
	imageCheckAttempts    = 3
)

var (
	// errCheckInterrupted fails the images of a manual check that a restart interrupted.
	errCheckInterrupted       = errors.New("image check was interrupted by a restart; run it again")
	imageCheckRetryBackoff    = 30 * time.Second
	imageCheckRetryBackoffMax = 2 * time.Minute
)

type ImageUpdateService struct {
	db                  *database.DB
	settingsService     *settings.SettingsService
	registryService     *registry.ContainerRegistryService
	dockerService       *docker.DockerClientService
	eventService        *event.EventService
	notificationService *notification.NotificationService
	registryLimiter     *ratelimit.RegistryRateLimiter
	tags                *tags.Service
	activityService     *activity.ActivityService
	engine              *flow.Engine
	checkWorkflow       *flow.Workflow
	credentials         *hot.HotCache[string, []containerregistry.Credential]
	// requestCredentials holds credentials a caller supplied for one check, by its input's CredentialsKey,
	// so they never reach the workflow journal. They expire with the check's timeout if nothing deletes them.
	requestCredentials *hot.HotCache[string, []containerregistry.Credential]
	notifyMu           sync.Mutex
}

type ImageParts struct {
	Registry   string
	Repository string
	Tag        string
}

// imageCheckResultInternal is a check task's output; failures travel as errors.
type imageCheckResultInternal struct {
	Local bool `json:"local,omitempty"`
	// Result is the ref's own result, kept when the caller reports results; tags sharing an image share its stored row.
	Result *imageupdate.Response `json:"result,omitempty"`
	// Failure is a reported check's error, which keeps its Result instead of failing the slot.
	Failure string `json:"failure,omitempty"`
}

// imageCheckInput is an image-check payload; Report keeps every ref's result in the output for the caller.
type imageCheckInput struct {
	imageupdate.CheckRequest
	Report         bool   `json:"report,omitempty"`
	CredentialsKey string `json:"credentialsKey,omitempty"`
}

// imageCheckOutput is the image-check outcome plus, when reported, each ref's result and the current
// container tag results, failed checks included.
type imageCheckOutput struct {
	scheduler.Outcome
	Results          imageupdate.BatchResponse        `json:"results,omitempty"`
	ContainerUpdates map[string]*imageupdate.Response `json:"containerUpdates,omitempty"`
}

// tagCheckOutput is the tags step's output: failed container checks and, when reported, every container's result.
type tagCheckOutput struct {
	Failures []scheduler.TargetOutcome        `json:"failures"`
	Updates  map[string]*imageupdate.Response `json:"updates,omitempty"`
}

type localImageSnapshot struct {
	ImageID           string
	Repository        string
	Tag               string
	PrimaryDigest     string
	AllDigests        []string
	RepositoryDigests []string
	IsLocalBuild      bool
}

func NewImageUpdateService(
	db *database.DB,
	settingsService *settings.SettingsService,
	registryService *registry.ContainerRegistryService,
	dockerService *docker.DockerClientService,
	eventService *event.EventService,
	notificationService *notification.NotificationService,
	activityService *activity.ActivityService,
) *ImageUpdateService {
	registryLimiter := ratelimit.NewRegistryRateLimiter()
	return &ImageUpdateService{
		db:                  db,
		settingsService:     settingsService,
		registryService:     registryService,
		dockerService:       dockerService,
		eventService:        eventService,
		notificationService: notificationService,
		registryLimiter:     registryLimiter,
		tags:                tags.NewService(registryService, dockerService, settingsService, registryLimiter),
		activityService:     activityService,
		credentials:         hot.NewHotCache[string, []containerregistry.Credential](hot.LRU, 8).WithTTL(time.Minute).Build(),
		// They outlive the longest check: its slot wait plus its workflow timeout.
		requestCredentials: hot.NewHotCache[string, []containerregistry.Credential](hot.LRU, 64).
			WithTTL(timeouts.DefaultActivitySlotWait + timeouts.DefaultImageUpdateScan).Build(),
	}
}

func (s *ImageUpdateService) dockerAPIContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	timeoutSeconds := 0
	if s != nil && s.settingsService != nil {
		timeoutSeconds = s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt()
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultDockerAPI))
}

func (s *ImageUpdateService) registryContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	timeoutSeconds := 0
	if s != nil && s.settingsService != nil {
		timeoutSeconds = s.settingsService.GetSettingsConfig().RegistryTimeout.AsInt()
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultRegistry))
}

func (s *ImageUpdateService) dockerClientInternal(ctx context.Context) (*client.Client, error) {
	if s == nil || s.dockerService == nil {
		return nil, errors.New("docker service unavailable")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	return dockerClient, nil
}

func (s *ImageUpdateService) composeBuildImageRefsInternal(ctx context.Context) (map[string]struct{}, error) {
	buildRefs := make(map[string]struct{})
	if s == nil || s.db == nil {
		return buildRefs, nil
	}

	var projectRows []struct {
		ID                 string
		BuildImageRefsJSON *string `gorm:"column:build_image_refs_json"`
	}
	if err := s.db.WithContext(ctx).
		Table("projects").
		Select("id", "build_image_refs_json").
		Where("build_image_refs_json IS NOT NULL AND build_image_refs_json <> ''").
		Find(&projectRows).Error; err != nil {
		return nil, fmt.Errorf("load project build image references: %w", err)
	}

	for i := range projectRows {
		if projectRows[i].BuildImageRefsJSON == nil {
			continue
		}
		for _, imageRef := range projects.ParseImageRefsJSON(*projectRows[i].BuildImageRefsJSON) {
			normalized := refs.NormalizeImageUpdateRef(imageRef)
			if normalized != "" {
				buildRefs[normalized] = struct{}{}
			}
		}
	}

	// Copacetic-patched tags only exist in the local daemon, so treat them like
	// locally built images instead of failing a remote registry lookup. Never
	// fail the update check over patch history.
	var patchedRefs []string
	if err := s.db.WithContext(ctx).
		Table("image_patches").
		Where("status = ?", "completed").
		Distinct().
		Pluck("patched_ref", &patchedRefs).Error; err != nil {
		slog.DebugContext(ctx, "failed to load patched image references", "error", err)
	}
	for _, imageRef := range patchedRefs {
		normalized := refs.NormalizeImageUpdateRef(imageRef)
		if normalized != "" {
			buildRefs[normalized] = struct{}{}
		}
	}

	return buildRefs, nil
}

func (s *ImageUpdateService) startImageUpdateActivityInternal(ctx context.Context, imageRef string) string {
	if s.activityService == nil {
		return ""
	}
	localActivity, err := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
		EnvironmentID: "0",
		Type:          activitytypes.TypeImageUpdateCheck,
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
}

// appendImageUpdateActivityMessageInternal appends a check message; a nil
// progress leaves progress to the activity's owner, such as the workflow engine.
func (s *ImageUpdateService) appendImageUpdateActivityMessageInternal(ctx context.Context, activityID string, level activitytypes.MessageLevel, message string, progress *int, step string) {
	if s.activityService == nil || activityID == "" || strings.TrimSpace(message) == "" {
		return
	}
	level = cmp.Or(level, activitytypes.MessageLevelInfo)
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    level,
		Message:  message,
		Progress: progress,
		Step:     step,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append image update activity message", "activityId", activityID, "error", err)
	}
}

func (s *ImageUpdateService) completeImageUpdateActivityInternal(ctx context.Context, activityID string, success bool, message string) {
	if s.activityService == nil || activityID == "" {
		return
	}
	status := activitytypes.StatusSuccess
	var errMessage *string
	if !success {
		status = activitytypes.StatusFailed
		errMessage = mo.EmptyableToOption(strings.TrimSpace(message)).ToPointer()
		if activitylib.CancelledByContext(ctx) {
			status = activitytypes.StatusCancelled
			errMessage = nil
			message = "Image update check cancelled"
		}
	}
	message = cmp.Or(message, "Image update check completed")
	step := "Image update check complete"
	if _, err := s.activityService.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errMessage, step); err != nil {
		// A lost terminal write strands the activity in running forever, so it
		// must be loud enough to correlate with a stuck activity panel entry.
		slog.ErrorContext(ctx, "failed to complete image update activity", "activityId", activityID, "error", err)
	}
}

func (s *ImageUpdateService) CheckImageUpdate(ctx context.Context, imageRef string) (*imageupdate.Response, error) {
	startTime := time.Now()
	activityID := s.startImageUpdateActivityInternal(ctx, imageRef)
	ctx = s.activityService.Track(ctx, activityID)
	activitylib.AwaitHandlerActivitySlot(ctx, s.activityService, activityID, "0")

	if result, ok := digestPinnedImageUpdateResultInternal(imageRef).Get(); ok {
		result.ResponseTimeMs = int(time.Since(startTime).Milliseconds())
		result.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
		s.recordDigestPinnedSkipInternal(ctx, activityID, imageRef, result, new(100))
		if s.eventService != nil {
			metadata := database.JSON{
				"action":         "check_update",
				"imageRef":       imageRef,
				"hasUpdate":      false,
				"updateType":     UpdateTypeDigest,
				"currentDigest":  result.CurrentDigest,
				"latestDigest":   result.LatestDigest,
				"responseTimeMs": result.ResponseTimeMs,
				"skippedReason":  "digest_pinned",
			}
			if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, "", imageRef, user.SystemUser.ID, user.SystemUser.Username, "0", metadata); logErr != nil {
				slog.WarnContext(ctx, "Failed to log digest-pinned image update check event", "imageRef", imageRef, "error", logErr.Error())
			}
		}
		s.completeImageUpdateActivityInternal(ctx, activityID, true, "Image update check skipped")
		return result, nil
	}

	s.appendImageUpdateActivityMessageInternal(ctx, activityID, activitytypes.MessageLevelInfo, "Checking "+imageRef, new(20), "Checking remote digest")

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		result := &imageupdate.Response{
			Error:          "Invalid image reference format",
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}
		s.completeImageUpdateActivityInternal(ctx, activityID, false, result.Error)
		return result, nil
	}

	composeBuildRefs, err := s.composeBuildImageRefsInternal(ctx)
	var digestResult *imageupdate.Response
	var snapshot *localImageSnapshot
	if err == nil {
		digestResult, snapshot, err = s.checkDigestUpdateWithSnapshotInternal(ctx, parts, composeBuildRefs)
	}
	if err == nil && digestResult == nil {
		err = errors.New("digest update check returned no result")
	}
	var tagRefs []string
	if digestResult == nil || digestResult.UpdateType != UpdateTypeLocal {
		tagRefs = append(tagRefs, imageRef)
	}
	containerUpdates, tagErr := s.checkContainerTagUpdatesInternal(ctx, tagRefs, nil)
	if tagErr != nil {
		s.completeImageUpdateActivityInternal(ctx, activityID, false, tagErr.Error())
		return digestResult, tagErr
	}
	if err != nil {
		result := &imageupdate.Response{
			Error:          err.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}
		metadata := database.JSON{
			"action":    "check_update",
			"imageRef":  imageRef,
			"error":     err.Error(),
			"checkType": "digest",
		}
		if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, "", imageRef, user.SystemUser.ID, user.SystemUser.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check error event", "imageRef", imageRef, "error", logErr.Error())
		}
		if saveErr := s.saveUpdateResultWithSnapshotInternal(ctx, imageRef, result, snapshot); saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
		}
		tags.AttachContainerUpdates(map[string]*imageupdate.Response{imageRef: result}, containerUpdates)
		s.completeImageUpdateActivityInternal(ctx, activityID, false, result.Error)
		if result.HasUpdate {
			// A failed flush is already logged; the records stay pending for the next one.
			_ = s.SendBatchUpdateNotifications(ctx)
			return result, nil
		}
		return result, err
	}

	digestResult.ResponseTimeMs = int(time.Since(startTime).Milliseconds())
	digestResult.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	if digestResult.UpdateType == UpdateTypeLocal {
		s.appendImageUpdateActivityMessageInternal(ctx, activityID, activitytypes.MessageLevelInfo, imageRef+" — local build, registry check skipped", new(100), "Skipping image update check")
	}
	metadata := database.JSON{
		"action":         "check_update",
		"imageRef":       imageRef,
		"hasUpdate":      digestResult.HasUpdate,
		"updateType":     digestResult.UpdateType,
		"currentDigest":  digestResult.CurrentDigest,
		"latestDigest":   digestResult.LatestDigest,
		"responseTimeMs": digestResult.ResponseTimeMs,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, "", imageRef, user.SystemUser.ID, user.SystemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "Failed to log image update check event", "imageRef", imageRef, "error", logErr.Error())
	}
	if saveErr := s.saveUpdateResultWithSnapshotInternal(ctx, imageRef, digestResult, snapshot); saveErr != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
	}

	s.notifyImageUpdateInternal(ctx, imageRef, digestResult, snapshot)
	tags.AttachContainerUpdates(map[string]*imageupdate.Response{imageRef: digestResult}, containerUpdates)
	if len(containerUpdates) > 0 {
		// A failed flush is already logged; the records stay pending for the next one.
		_ = s.SendBatchUpdateNotifications(ctx)
	}

	finalMessage := kit.Ternary(digestResult.HasUpdate, "Image update available", "Image update check completed")
	s.completeImageUpdateActivityInternal(ctx, activityID, true, finalMessage)
	return digestResult, nil
}

// notifyImageUpdateInternal sends the single-image update notification and marks the
// record notified once it was delivered to at least one provider. A partial provider
// failure still counts as notified — the user saw the update, and re-sending on every
// poll would duplicate it forever; the failure stays visible in the delivery history.
// Only a total failure (delivered == 0) leaves the record unnotified for retry.
// Prefer the snapshot's image ID: it is the same key
// saveUpdateResultWithSnapshotInternal stored the record under.
func (s *ImageUpdateService) notifyImageUpdateInternal(ctx context.Context, imageRef string, digestResult *imageupdate.Response, snapshot *localImageSnapshot) {
	if !digestResult.HasUpdate || s.notificationService == nil {
		return
	}

	delivered, notifErr := s.notificationService.SendImageUpdateNotification(ctx, imageRef, digestResult, notifications.NotificationEventImageUpdate)
	if notifErr != nil {
		slog.WarnContext(ctx, "Failed to send update notification", "imageRef", imageRef, "error", notifErr.Error())
	}
	if delivered == 0 {
		return
	}

	imageID := s.resolveNotifiedImageIDInternal(ctx, imageRef, snapshot)
	if imageID == "" {
		return
	}
	if markErr := s.MarkUpdatesAsNotified(ctx, []string{imageID}); markErr != nil {
		slog.WarnContext(ctx, "Failed to mark update as notified", "imageRef", imageRef, "error", markErr.Error())
	}
}

// resolveNotifiedImageIDInternal returns the image ID used to mark an update
// notified, preferring the snapshot's image ID and falling back to a lookup by
// reference.
func (s *ImageUpdateService) resolveNotifiedImageIDInternal(ctx context.Context, imageRef string, snapshot *localImageSnapshot) string {
	if snapshot != nil && snapshot.ImageID != "" {
		return snapshot.ImageID
	}
	resolved, err := s.getImageIDByRef(ctx, imageRef)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve image ID to mark notified", "imageRef", imageRef, "error", err.Error())
		return ""
	}
	return resolved
}

func digestPinnedImageUpdateResultInternal(imageRef string) mo.Option[*imageupdate.Response] {
	imageRef = strings.TrimSpace(imageRef)
	pinnedDigest, ok := digest.FromReferenceSuffix(imageRef)
	if !ok {
		return mo.None[*imageupdate.Response]()
	}

	tag := "latest"
	if named, err := reference.ParseNormalizedNamed(imageRef); err == nil {
		if tagged, localOk := named.(reference.NamedTagged); localOk {
			tag = tagged.Tag()
		}
	}

	return mo.Some(&imageupdate.Response{
		HasUpdate:      false,
		UpdateType:     UpdateTypeDigest,
		CurrentVersion: tag,
		LatestVersion:  tag,
		CurrentDigest:  pinnedDigest,
		LatestDigest:   pinnedDigest,
		CheckTime:      time.Now(),
		ResponseTimeMs: 0,
	})
}

func isLocalBuildImageRefInternal(imageRef string, composeBuildRefs map[string]struct{}) bool {
	if named, err := reference.ParseNormalizedNamed(strings.TrimSpace(imageRef)); err == nil &&
		registryauth.NormalizeRegistryForComparison(reference.Domain(named)) == imageref.LocalBuildRegistry {
		return true
	}
	_, ok := composeBuildRefs[refs.NormalizeImageUpdateRef(imageRef)]
	return ok
}

func (s *ImageUpdateService) checkDigestUpdateWithSnapshotInternal(ctx context.Context, parts *ImageParts, composeBuildRefs map[string]struct{}) (*imageupdate.Response, *localImageSnapshot, error) {
	if s.registryService == nil {
		return nil, nil, errors.New("registry service unavailable")
	}

	imageRef := fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag)
	start := time.Now()
	snapshot, err := s.inspectLocalImageSnapshotInternal(ctx, imageRef, composeBuildRefs)
	if err == nil && snapshot.IsLocalBuild {
		return localBuildImageUpdateResultInternal(snapshot, int(time.Since(start).Milliseconds())), snapshot, nil
	}
	if err != nil && errdefs.IsNotFound(err) && isLocalBuildImageRefInternal(imageRef, composeBuildRefs) {
		return missingLocalBuildImageUpdateResultInternal(parts.Tag, int(time.Since(start).Milliseconds())), nil, nil
	}

	registryCtx, registryCancel := s.registryContextInternal(ctx)
	digestResult, err := s.registryService.InspectImageDigest(registryCtx, imageRef, nil)
	registryCancel()
	elapsed := time.Since(start)
	if err != nil {
		partial := digestResult // may contain auth metadata even on error
		if partial == nil {
			return nil, nil, fmt.Errorf("failed to get remote digest: %w", err)
		}
		return &imageupdate.Response{
			Error:          err.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(elapsed.Milliseconds()),
			AuthMethod:     partial.AuthMethod,
			AuthUsername:   partial.AuthUsername,
			AuthRegistry:   partial.AuthRegistry,
			UsedCredential: partial.UsedCredential,
		}, nil, fmt.Errorf("failed to get remote digest: %w", err)
	}

	if snapshot == nil {
		snapshot, err = s.inspectLocalImageSnapshotInternal(ctx, imageRef, composeBuildRefs)
		if err != nil {
			if errdefs.IsNotFound(err) {
				// The ref resolved remotely but was never pulled locally: there is
				// nothing local to compare, so report a distinct not-pulled state
				// rather than a failed check or an available update.
				return &imageupdate.Response{
					UpdateType:     UpdateTypeNotPulled,
					LatestDigest:   digestResult.Digest,
					CheckTime:      time.Now(),
					ResponseTimeMs: int(elapsed.Milliseconds()),
					AuthMethod:     digestResult.AuthMethod,
					AuthUsername:   digestResult.AuthUsername,
					AuthRegistry:   digestResult.AuthRegistry,
					UsedCredential: digestResult.UsedCredential,
				}, nil, nil
			}
			return nil, nil, fmt.Errorf("failed to get local digest: %w", err)
		}
	}

	// This comparison is deliberately index-level: local RepoDigests record the
	// multi-platform index digest a tag resolved to at pull time, and the
	// registry digest above is the same index-level value — "update available"
	// answers "would a pull fetch something new for this tag". Compose (v5.5.0+)
	// instead recreates containers based on the platform-specific manifest
	// digest in its com.docker.compose.image label, so a multi-arch tag whose
	// *other* platform changed can show an update here while a redeploy
	// correctly recreates nothing; the pull still clears the badge. Do not
	// "align" this to platform-level digests — that would require per-platform
	// registry manifest fetches for no user-visible gain.
	localDigest := snapshot.PrimaryDigest
	hasUpdate := true
	for _, localDig := range snapshot.AllDigests {
		if localDig == digestResult.Digest {
			localDigest = localDig
			hasUpdate = false
			break
		}
	}

	slog.DebugContext(ctx, "digest comparison",
		"imageRef", imageRef,
		"primaryLocalDigest", localDigest,
		"allLocalDigests", snapshot.AllDigests,
		"remoteDigest", digestResult.Digest,
		"hasUpdate", hasUpdate)

	return &imageupdate.Response{
		HasUpdate:      hasUpdate,
		UpdateType:     UpdateTypeDigest,
		CurrentDigest:  localDigest,
		LatestDigest:   digestResult.Digest,
		CheckTime:      time.Now(),
		ResponseTimeMs: int(elapsed.Milliseconds()),
		AuthMethod:     digestResult.AuthMethod,
		AuthUsername:   digestResult.AuthUsername,
		AuthRegistry:   digestResult.AuthRegistry,
		UsedCredential: digestResult.UsedCredential,
	}, snapshot, nil
}

func localBuildImageUpdateResultInternal(snapshot *localImageSnapshot, responseTimeMs int) *imageupdate.Response {
	return &imageupdate.Response{
		HasUpdate:      false,
		UpdateType:     UpdateTypeLocal,
		CurrentVersion: snapshot.Tag,
		CurrentDigest:  snapshot.PrimaryDigest,
		CheckTime:      time.Now(),
		ResponseTimeMs: responseTimeMs,
	}
}

// missingLocalBuildImageUpdateResultInternal reports a compose build ref whose
// image is not present locally: there is no registry to check against, so the
// registry lookup is skipped entirely.
func missingLocalBuildImageUpdateResultInternal(tag string, responseTimeMs int) *imageupdate.Response {
	return &imageupdate.Response{
		HasUpdate:      false,
		UpdateType:     UpdateTypeLocal,
		CurrentVersion: tag,
		CheckTime:      time.Now(),
		ResponseTimeMs: responseTimeMs,
	}
}

func (s *ImageUpdateService) parseImageReference(imageRef string) *ImageParts {
	// Use the official Docker reference parser to handle all edge cases
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		// Fallback to manual parsing if the official parser fails
		return s.parseImageReferenceFallback(imageRef)
	}

	// Extract registry
	registryHost := reference.Domain(named)

	// Extract repository (path without registry)
	repository := reference.Path(named)

	// Extract tag or default to latest
	tag := "latest"
	if tagged, ok := named.(reference.NamedTagged); ok {
		tag = tagged.Tag()
	}

	return &ImageParts{
		Registry:   registryauth.NormalizeRegistryForComparison(registryHost),
		Repository: repository,
		Tag:        tag,
	}
}

// Fallback parser for cases where the official parser fails
func (s *ImageUpdateService) parseImageReferenceFallback(imageRef string) *ImageParts {
	var registryHost, repository, tag string
	if _, ok := digest.FromReferenceSuffix(imageRef); ok {
		digestParts := strings.Split(imageRef, "@")
		if len(digestParts) != 2 {
			return nil
		}
		repoWithRegistry := digestParts[0]
		parts := strings.Split(repoWithRegistry, "/")
		if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
			registryHost = parts[0]
			repository = strings.Join(parts[1:], "/")
		} else {
			registryHost = "docker.io"
			if len(parts) == 1 {
				repository = "library/" + parts[0]
			} else {
				repository = repoWithRegistry
			}
		}
		tag = "latest"
	} else {
		parts := strings.Split(imageRef, "/")
		switch {
		case len(parts) == 1:
			registryHost = "docker.io"
			if strings.Contains(parts[0], ":") {
				repoParts := strings.Split(parts[0], ":")
				repository = "library/" + repoParts[0]
				tag = repoParts[1]
			} else {
				repository = "library/" + parts[0]
				tag = "latest"
			}
		case strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":"):
			registryHost = parts[0]
			repository = strings.Join(parts[1:], "/")
			if strings.Contains(repository, ":") {
				repoParts := strings.Split(repository, ":")
				repository = repoParts[0]
				tag = repoParts[1]
			} else {
				tag = "latest"
			}
		default:
			registryHost = "docker.io"
			repository = imageRef
			if strings.Contains(repository, ":") {
				repoParts := strings.Split(repository, ":")
				repository = repoParts[0]
				tag = repoParts[1]
			} else {
				tag = "latest"
			}
		}
	}
	return &ImageParts{Registry: registryauth.NormalizeRegistryForComparison(registryHost), Repository: repository, Tag: tag}
}

func (s *ImageUpdateService) getImageRefByIDInternal(ctx context.Context, imageID string) (string, error) {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return "", err
	}

	imageID = strings.TrimPrefix(imageID, "sha256:")

	if imageRef, refErr := s.resolveImageRefFromInspect(ctx, dockerClient, imageID); refErr == nil {
		return imageRef, nil
	}

	// Fallback: if the image was pruned, look up the image reference from
	// running containers that were started from this image ID.
	if imageRef, refErr := s.resolveImageRefFromContainers(ctx, dockerClient, imageID); refErr == nil {
		return imageRef, nil
	}

	return "", fmt.Errorf("image not found: no local image or running container found for %s", imageID)
}

func (s *ImageUpdateService) resolveImageRefFromInspect(ctx context.Context, dockerClient client.APIClient, imageID string) (string, error) {
	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	defer cancel()

	inspectResponse, err := dockerClient.ImageInspect(apiCtx, imageID)
	if err != nil {
		return "", err
	}
	for _, tag := range inspectResponse.RepoTags {
		if tag != "<none>:<none>" {
			return tag, nil
		}
	}
	for _, digestValue := range inspectResponse.RepoDigests {
		if digestValue != "<none>@<none>" {
			if repo, _, ok := strings.Cut(digestValue, "@"); ok {
				return repo + ":latest", nil
			}
		}
	}
	return "", errors.New("no valid tags or digests")
}

func (s *ImageUpdateService) resolveImageRefFromContainers(ctx context.Context, dockerClient client.APIClient, imageID string) (string, error) {
	fullID := "sha256:" + imageID
	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	defer cancel()

	containers, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	if err != nil {
		return "", err
	}
	for _, c := range containers.Items {
		if c.ImageID != fullID && c.ImageID != imageID {
			continue
		}
		if c.Image != "" && !strings.HasPrefix(c.Image, "sha256:") && !strings.Contains(c.Image, "@sha256:") {
			return c.Image, nil
		}
	}
	return "", fmt.Errorf("no container found using image %s", imageID)
}

func (s *ImageUpdateService) getAllImageRefsInternal(ctx context.Context, limit int) ([]string, error) {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return nil, err
	}

	imageCtx, cancelImage := s.dockerAPIContextInternal(ctx)
	imageList, err := dockerClient.ImageList(imageCtx, client.ImageListOptions{})
	cancelImage()
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker images: %w", err)
	}

	containerCtx, cancelContainers := s.dockerAPIContextInternal(ctx)
	containerList, err := dockerClient.ContainerList(containerCtx, client.ContainerListOptions{All: true})
	cancelContainers()
	if err != nil {
		slog.WarnContext(ctx, "failed to list Docker containers; continuing without update-check opt-out filtering", "error", err.Error())
		return imageRefsFromSummariesInternal(imageList.Items, limit), nil
	}

	return filterImageSummariesByContainerOptOutInternal(imageList.Items, containerList.Items, limit), nil
}

func imageRefsFromSummariesInternal(images []image.Summary, limit int) []string {
	seen := make(map[string]struct{})
	imageRefs := make([]string, 0)

	for _, summary := range images {
		for _, imageRef := range summary.RepoTags {
			if imageRef == "<none>:<none>" {
				continue
			}
			if _, exists := seen[imageRef]; exists {
				continue
			}

			seen[imageRef] = struct{}{}
			imageRefs = append(imageRefs, imageRef)
			if limit > 0 && len(imageRefs) >= limit {
				return imageRefs
			}
		}
	}

	return imageRefs
}

// monitoringEligibilityInternal records, per image ID and normalized
// reference, whether at least one consuming container still permits update
// checks. Only the update-check label is consulted: containers excluded from
// automatic installation keep being monitored.
type monitoringEligibilityInternal struct {
	// True when at least one container using the image ID or reference has
	// not opted out; present but false when every such container opted out.
	byImageID   map[string]bool
	byRef       map[string]bool
	byContainer map[string]bool
}

func newMonitoringEligibilityInternal(containers []container.Summary) monitoringEligibilityInternal {
	eligibility := monitoringEligibilityInternal{byImageID: map[string]bool{}, byRef: map[string]bool{}, byContainer: map[string]bool{}}
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

// imageEligible reports whether an image may be checked: unused images are
// eligible, used images need one consumer that has not opted out.
func (e monitoringEligibilityInternal) imageEligible(imageID, normalizedRef string) bool {
	eligibleByID, usedByID := e.byImageID[strings.TrimSpace(imageID)]
	eligibleRef, usedByRef := e.byRef[normalizedRef]
	return (!usedByID && !usedByRef) || eligibleByID || eligibleRef
}

// recordEligible reports whether a stored result may still be reported. Results
// scoped to a container that no longer exists are stale and not eligible.
func (e monitoringEligibilityInternal) recordEligible(record *ImageUpdateRecord) bool {
	if record.ContainerID != "" {
		return e.byContainer[record.ContainerID]
	}
	return e.imageEligible(record.ID, refs.NormalizeImageUpdateRef(record.Repository+":"+record.Tag))
}

func filterImageSummariesByContainerOptOutInternal(images []image.Summary, containers []container.Summary, limit int) []string {
	eligibility := newMonitoringEligibilityInternal(containers)

	seen := make(map[string]struct{})
	filtered := make([]string, 0)

	// Equivalent spellings share one candidate; the first one seen is kept.
	// Digest-pinned refs do not normalize, so they dedupe on their own spelling.
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
			if addCandidate(imageRef, summary.ID) {
				return filtered
			}
		}
	}

	// Containers can run images whose tag is no longer present locally, so
	// their references are candidates too.
	for _, summary := range containers {
		if addCandidate(summary.Image, summary.ImageID) {
			return filtered
		}
	}

	return filtered
}

func (s *ImageUpdateService) inspectLocalImageSnapshotInternal(ctx context.Context, imageRef string, composeBuildRefs map[string]struct{}) (*localImageSnapshot, error) {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return nil, err
	}

	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	defer cancel()

	inspectResponse, err := dockerClient.ImageInspect(apiCtx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}

	var allDigests, repositoryDigests []string
	var primaryDigest string
	isLocalBuild := false
	requested, referenceErr := refs.NormalizeReference(imageRef)

	// Extract all digests from RepoDigests
	if len(inspectResponse.RepoDigests) > 0 {
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

			// Use first digest as primary if not yet set
			primaryDigest = cmp.Or(primaryDigest, digestValue)
		}
	}

	if len(repositoryDigests) > 0 {
		primaryDigest = repositoryDigests[0]
	}

	// Fallback to image ID if no repo digests available
	if primaryDigest == "" {
		isLocalBuild = true
		primaryDigest = inspectResponse.ID
		allDigests = []string{primaryDigest}
	}
	isLocalBuild = isLocalBuild || isLocalBuildImageRefInternal(imageRef, composeBuildRefs)

	repo, tag := extractRepoAndTagFromImage(inspectResponse.InspectResponse)
	tag = tagWithFallbackInternal(tag, s.parseImageReference(imageRef))

	return &localImageSnapshot{
		ImageID:           inspectResponse.ID,
		Repository:        repo,
		Tag:               tag,
		PrimaryDigest:     primaryDigest,
		AllDigests:        allDigests,
		RepositoryDigests: repositoryDigests,
		IsLocalBuild:      isLocalBuild,
	}, nil
}

func (s *ImageUpdateService) CheckImageUpdateByID(ctx context.Context, imageID string) (*imageupdate.Response, error) {
	imageRef, err := s.getImageRefByIDInternal(ctx, imageID)
	if err != nil {
		metadata := database.JSON{
			"action":  "check_update_by_id",
			"imageID": imageID,
			"error":   err.Error(),
		}
		if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, imageID, "", user.SystemUser.ID, user.SystemUser.Username, "0", metadata); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check by ID error event", "imageId", imageID, "error", logErr.Error())
		}
		return nil, fmt.Errorf("failed to get image reference: %w", err)
	}
	result, err := s.CheckImageUpdate(ctx, imageRef)
	if err != nil {
		return nil, err
	}
	if len(result.ContainerUpdates) == 0 {
		if saveErr := s.saveUpdateResultByIDInternal(ctx, imageID, result, s.parseImageReference(imageRef)); saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result by ID", "imageId", imageID, "error", saveErr.Error())
		}
	}
	return result, nil
}

func (s *ImageUpdateService) saveUpdateResultWithSnapshotInternal(ctx context.Context, imageRef string, result *imageupdate.Response, snapshot *localImageSnapshot) error {
	if snapshot != nil && snapshot.ImageID != "" {
		return s.savePreparedUpdateResultInternal(ctx, snapshot.ImageID, snapshot.Repository, snapshot.Tag, result)
	}

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		return errors.New("invalid image reference")
	}
	imageID, err := s.getImageIDByRef(ctx, imageRef)
	if err != nil {
		repository := buildImageUpdateRepositoryInternal(parts)
		syntheticID := fmt.Sprintf("ref::%s@%s", strings.ToLower(strings.TrimSpace(repository)), strings.TrimSpace(parts.Tag))
		slog.DebugContext(ctx, "Saving image update result with synthetic ref ID",
			"imageRef", imageRef,
			"error", err.Error(),
			"repository", repository,
			"tag", parts.Tag,
			"syntheticId", syntheticID)
		// Persist registry results even when the local image no longer exists. This keeps
		// project/image update status available for pruned images using a ref-scoped record.
		return s.savePreparedUpdateResultInternal(ctx, syntheticID, repository, parts.Tag, result)
	}

	return s.saveUpdateResultByIDInternal(ctx, imageID, result, parts)
}

func buildImageUpdateRepositoryInternal(parts *ImageParts) string {
	if parts == nil {
		return ""
	}

	repository := strings.TrimSpace(parts.Repository)
	if parts.Registry == "docker.io" && repository != "" && !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	if strings.TrimSpace(parts.Registry) == "" {
		return repository
	}

	return fmt.Sprintf("%s/%s", strings.TrimSpace(parts.Registry), repository)
}

// imageCheckResultMessageInternal derives an activity message level and text from
// a per-image update check result: errors become ERROR, available updates become
// SUCCESS, and up-to-date images stay INFO.
func imageCheckResultMessageInternal(imageRef string, res *imageupdate.Response) (activitytypes.MessageLevel, string) {
	if res == nil {
		return activitytypes.MessageLevelError, imageRef + ": check failed"
	}
	if err := strings.TrimSpace(res.Error); err != "" {
		return activitytypes.MessageLevelError, fmt.Sprintf("%s: %s", imageRef, err)
	}
	if res.UpdateType == UpdateTypeLocal {
		return activitytypes.MessageLevelInfo, imageRef + " — local build, registry check skipped"
	}
	if res.UpdateType == UpdateTypeNotPulled {
		return activitytypes.MessageLevelInfo, imageRef + " — not pulled locally, no local digest to compare"
	}
	if res.HasUpdate {
		return activitytypes.MessageLevelSuccess, imageRef + " — update available"
	}
	return activitytypes.MessageLevelInfo, imageRef + " — up to date"
}

func extractRepoAndTagFromImage(dockerImage image.InspectResponse) (repo, tag string) {
	if len(dockerImage.RepoTags) > 0 && dockerImage.RepoTags[0] != "<none>:<none>" {
		if named, err := reference.ParseNormalizedNamed(dockerImage.RepoTags[0]); err == nil {
			repo = reference.FamiliarName(named)
			if tagged, ok := named.(reference.NamedTagged); ok {
				tag = tagged.Tag()
			} else {
				tag = "latest"
			}
			return repo, tag
		}

		parts := strings.SplitN(dockerImage.RepoTags[0], ":", 2)
		repo = parts[0]
		if len(parts) > 1 {
			tag = parts[1]
		} else {
			tag = "latest"
		}
		return repo, tag
	}

	if len(dockerImage.RepoDigests) > 0 {
		for _, rd := range dockerImage.RepoDigests {
			if rd == "<none>@<none>" {
				continue
			}
			if repoCandidate, _, found := strings.CutLast(rd, "@"); found && repoCandidate != "" {
				return repoCandidate, "<none>"
			}
		}
	}

	return "<none>", "<none>"
}

func buildImageUpdateRecord(imageID, repo, tag string, result *imageupdate.Response) *ImageUpdateRecord {
	currentVersion := cmp.Or(result.CurrentVersion, tag)

	return &ImageUpdateRecord{
		ID:             imageID,
		Repository:     repo,
		Tag:            tag,
		HasUpdate:      result.HasUpdate,
		UpdateType:     result.UpdateType,
		CurrentVersion: currentVersion,
		LatestVersion:  mo.EmptyableToOption(result.LatestVersion).ToPointer(),
		CurrentDigest:  mo.EmptyableToOption(result.CurrentDigest).ToPointer(),
		LatestDigest:   mo.EmptyableToOption(result.LatestDigest).ToPointer(),
		CheckTime:      result.CheckTime,
		ResponseTimeMs: result.ResponseTimeMs,
		LastError:      mo.EmptyableToOption(result.Error).ToPointer(),
		AuthMethod:     mo.EmptyableToOption(result.AuthMethod).ToPointer(),
		AuthUsername:   mo.EmptyableToOption(result.AuthUsername).ToPointer(),
		AuthRegistry:   mo.EmptyableToOption(result.AuthRegistry).ToPointer(),
		UsedCredential: result.UsedCredential,
	}
}

func repositoryCandidatesSliceInternal(candidates map[string]struct{}) []string {
	if len(candidates) == 0 {
		return nil
	}

	repositories := slices.Collect(maps.Keys(candidates))
	return repositories
}

func savePreparedUpdateResultWithTxInternal(tx *gorm.DB, imageID, repo, tag string, result *imageupdate.Response) error {
	updateRecord := buildImageUpdateRecord(imageID, repo, tag, result)

	// Check if there's an existing record to compare state changes
	var existingRecord ImageUpdateRecord
	hasExisting := tx.Where("id = ?", imageID).First(&existingRecord).Error == nil

	// A registry rate limit (429) is transient: keep the previous good result
	// instead of clobbering it with an error record
	if hasExisting &&
		strings.TrimSpace(result.Error) != "" &&
		registry.IsRateLimitErrorString(result.Error) &&
		strings.TrimSpace(mo.PointerToOption(existingRecord.LastError).OrEmpty()) == "" {
		slog.DebugContext(tx.Statement.Context, "Preserving previous image update result; check hit a registry rate limit",
			"imageId", imageID, "repository", repo, "tag", tag, "error", result.Error)
		return nil
	}

	if hasExisting {
		// Existing record found - check if we need to reset notification_sent
		stateChanged := existingRecord.HasUpdate != updateRecord.HasUpdate
		digestChanged := mo.PointerToOption(existingRecord.LatestDigest).OrEmpty() != mo.PointerToOption(updateRecord.LatestDigest).OrEmpty()
		versionChanged := mo.PointerToOption(existingRecord.LatestVersion).OrEmpty() != mo.PointerToOption(updateRecord.LatestVersion).OrEmpty()

		// Reset notification_sent if the update state changed in any way
		if stateChanged || (updateRecord.HasUpdate && (digestChanged || versionChanged)) {
			updateRecord.NotificationSent = false
		} else {
			// Keep the existing notification_sent value if nothing changed
			updateRecord.NotificationSent = existingRecord.NotificationSent
		}
	} else {
		// New record - start with notification_sent = false
		updateRecord.NotificationSent = false
	}

	return tx.Save(updateRecord).Error
}

func (s *ImageUpdateService) saveUpdateResultByIDInternal(ctx context.Context, imageID string, result *imageupdate.Response, fallback *ImageParts) error {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return err
	}

	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	defer cancel()

	dockerImage, err := dockerClient.ImageInspect(apiCtx, imageID)
	if err != nil {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	repo, tag := extractRepoAndTagFromImage(dockerImage.InspectResponse)
	tag = tagWithFallbackInternal(tag, fallback)
	return s.savePreparedUpdateResultInternal(ctx, imageID, repo, tag, result)
}

func tagWithFallbackInternal(tag string, fallback *ImageParts) string {
	if tag == "<none>" && fallback != nil && strings.TrimSpace(fallback.Tag) != "" {
		return fallback.Tag
	}
	return tag
}

func (s *ImageUpdateService) savePreparedUpdateResultInternal(ctx context.Context, imageID, repo, tag string, result *imageupdate.Response) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return savePreparedUpdateResultWithTxInternal(tx, imageID, repo, tag, result)
	})
}

func (s *ImageUpdateService) getImageIDByRef(ctx context.Context, imageRef string) (string, error) {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return "", err
	}

	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	defer cancel()

	inspectResponse, err := dockerClient.ImageInspect(apiCtx, imageRef)
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	return inspectResponse.ID, nil
}

func (s *ImageUpdateService) MarkImageRefUpToDateAfterPull(ctx context.Context, imageRef string) error {
	if s.db == nil {
		return nil
	}

	snapshot, err := s.inspectLocalImageSnapshotInternal(ctx, imageRef, nil)
	if err != nil {
		return fmt.Errorf("inspect pulled image: %w", err)
	}

	checkTime := time.Now().UTC()
	result := &imageupdate.Response{
		HasUpdate:      false,
		UpdateType:     UpdateTypeDigest,
		CurrentVersion: snapshot.Tag,
		LatestVersion:  snapshot.Tag,
		CurrentDigest:  snapshot.PrimaryDigest,
		LatestDigest:   snapshot.PrimaryDigest,
		CheckTime:      checkTime,
		ResponseTimeMs: 0,
	}

	_, tag, repositoryCandidates, hasLookup := imageref.ParseUpdateLookup(imageRef)
	projectChecks, confirmedDigest, err := s.projectChecksAfterPull(ctx, imageRef, tag, repositoryCandidatesSliceInternal(repositoryCandidates), snapshot.RepositoryDigests)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if hasLookup {
			repositories := repositoryCandidatesSliceInternal(repositoryCandidates)
			if len(repositories) > 0 {
				// Only clear synthetic ref:: records, not real sha256 image ID records.
				// Clearing sha256 records would incorrectly mark containers that are still
				// running the old image as up-to-date (see: #2453).
				if clearStaleUpdatesErr := tx.Model(&ImageUpdateRecord{}).
					Where("id LIKE 'ref::%' AND tag = ? AND repository IN ?", tag, repositories).
					Update("has_update", false).Error; clearStaleUpdatesErr != nil {
					return fmt.Errorf("clear stale image updates: %w", clearStaleUpdatesErr)
				}
			}
		}
		for _, record := range projectChecks {
			// Record the local digest the preview targeted when the pull carries it.
			pulledDigest := cmp.Or(confirmedDigest, snapshot.RepositoryDigests[0])
			if previewDigest := mo.PointerToOption(record.LatestDigest).OrEmpty(); confirmedDigest == "" && slices.Contains(snapshot.RepositoryDigests, previewDigest) {
				pulledDigest = previewDigest
			}
			// Preserve any preview replaced while the registry lookup was in flight.
			if refreshProjectChecksErr := tx.Model(&ImageUpdateRecord{}).
				Where(map[string]any{
					"id": record.ID, "check_time": record.CheckTime, "policy_key": record.PolicyKey,
					"latest_digest": record.LatestDigest, "latest_version": record.LatestVersion,
					"current_digest": record.CurrentDigest, "last_error": record.LastError, "has_update": record.HasUpdate,
				}).
				Updates(map[string]any{
					"has_update":      false,
					"update_type":     UpdateTypeDigest,
					"current_version": tag,
					"latest_version":  tag,
					"current_digest":  pulledDigest,
					"latest_digest":   pulledDigest,
					"check_time":      checkTime,
				}).Error; refreshProjectChecksErr != nil {
				return fmt.Errorf("refresh project update checks: %w", refreshProjectChecksErr)
			}
		}

		if savePreparedUpdateResultWithTxErr := savePreparedUpdateResultWithTxInternal(tx, snapshot.ImageID, snapshot.Repository, snapshot.Tag, result); savePreparedUpdateResultWithTxErr != nil {
			return fmt.Errorf("save pulled image update state: %w", savePreparedUpdateResultWithTxErr)
		}

		return nil
	})
}

// projectChecksAfterPull returns the digest previews a pull of imageRef
// satisfies. Previews that were disabled, local, or not pulled carry no digest
// result and are left for the next project check.
func (s *ImageUpdateService) projectChecksAfterPull(ctx context.Context, imageRef, tag string, repositories, localDigests []string) ([]ImageUpdateRecord, string, error) {
	if len(repositories) == 0 || len(localDigests) == 0 {
		return nil, "", nil
	}
	var records []ImageUpdateRecord
	if err := s.db.WithContext(ctx).
		Where("project_id <> '' AND update_type = ? AND tag = ? AND repository IN ?", UpdateTypeDigest, tag, repositories).
		Where("latest_version IS NULL OR latest_version = '' OR latest_version = tag").
		Where("last_error IS NULL OR last_error = ''").Find(&records).Error; err != nil {
		return nil, "", fmt.Errorf("load project update checks after pull: %w", err)
	}
	digestMismatch := func(record ImageUpdateRecord) bool {
		latestDigest := mo.PointerToOption(record.LatestDigest).OrEmpty()
		return latestDigest != "" && !slices.Contains(localDigests, latestDigest)
	}
	if !slices.ContainsFunc(records, digestMismatch) {
		return records, "", nil
	}

	// Digests have no ordering. Confirm a moved tag without using the digest cache.
	// A registry failure leaves the differing previews untouched rather than
	// blocking the reconciliation of the pulled image state.
	if s.registryService != nil {
		registryCtx, cancel := s.registryContextInternal(ctx)
		defer cancel()
		latest, err := s.registryService.InspectImageDigest(registryCtx, imageRef, nil)
		if err != nil {
			slog.WarnContext(ctx, "Failed to verify pulled image against project previews", "imageRef", imageRef, "error", err.Error())
		} else if latest != nil && slices.Contains(localDigests, latest.Digest) {
			return records, latest.Digest, nil
		}
	}
	return slices.DeleteFunc(records, digestMismatch), "", nil
}

func (s *ImageUpdateService) StoredUpdateByImageID(ctx context.Context, imageID string) (*ImageUpdateRecord, bool, error) {
	imageID = strings.TrimSpace(imageID)
	if s == nil || s.db == nil || imageID == "" {
		return nil, false, nil
	}

	var record ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("id = ?", imageID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get stored image update by image id: %w", err)
	}

	return &record, true, nil
}

// GetUnnotifiedUpdates returns a map of image IDs that have updates but haven't been notified yet
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

// filterUnnotifiedByMonitoringInternal drops records whose resource no longer
// permits update checks, using the same eligibility rules as discovery. A
// failed container listing is returned so nothing is sent or marked.
func (s *ImageUpdateService) filterUnnotifiedByMonitoringInternal(ctx context.Context, records map[string]*ImageUpdateRecord) error {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return fmt.Errorf("resolve update-check eligibility: %w", err)
	}
	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	listed, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return fmt.Errorf("list containers for update-check eligibility: %w", err)
	}
	eligibility := newMonitoringEligibilityInternal(listed.Items)
	for id, record := range records {
		if !eligibility.recordEligible(record) {
			slog.DebugContext(ctx, "Skipping update notification for resource opted out of update checks", "recordId", id)
			delete(records, id)
		}
	}
	return nil
}

// MarkUpdatesAsNotified marks the given image IDs as having been notified
func (s *ImageUpdateService) MarkUpdatesAsNotified(ctx context.Context, imageIDs []string) error {
	if len(imageIDs) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).
		Model(&ImageUpdateRecord{}).
		Where("id IN ?", imageIDs).
		Update("notification_sent", true).Error
}

// groupImageRefsInternal dedupes refs naming the same registry/repo:tag into one
// check; the first ref of a group is canonical. Digest-pinned and invalid refs
// each form their own group.
func (s *ImageUpdateService) groupImageRefsInternal(imageRefs []string) [][]string {
	var groups [][]string
	indexByNormalizedRef := make(map[string]int)
	for _, imageRef := range imageRefs {
		parts := s.parseImageReference(imageRef)
		if parts == nil || digestPinnedImageUpdateResultInternal(imageRef).IsPresent() {
			groups = append(groups, []string{imageRef})
			continue
		}
		normalizedRef := strings.ToLower(fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag))
		if index, exists := indexByNormalizedRef[normalizedRef]; exists {
			groups[index] = append(groups[index], imageRef)
			continue
		}
		indexByNormalizedRef[normalizedRef] = len(groups)
		groups = append(groups, []string{imageRef})
	}
	return groups
}

func (
	s *ImageUpdateService,
) checkSingleImageInBatchInternal(
	ctx context.Context,
	externalCreds []containerregistry.Credential,
	parts *ImageParts,
	composeBuildRefs map[string]struct{},
) (
	*imageupdate.Response,
	*localImageSnapshot,
	error,
) {
	if s.registryService == nil {
		err := errors.New("registry service unavailable")
		return &imageupdate.Response{Error: err.Error(), CheckTime: time.Now()}, nil, err
	}

	start := time.Now()
	imageRef := fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag)
	snapshot, ldErr := s.inspectLocalImageSnapshotInternal(ctx, imageRef, composeBuildRefs)
	if ldErr == nil && snapshot.IsLocalBuild {
		return localBuildImageUpdateResultInternal(snapshot, int(time.Since(start).Milliseconds())), snapshot, nil
	}
	if ldErr != nil && errdefs.IsNotFound(ldErr) && isLocalBuildImageRefInternal(imageRef, composeBuildRefs) {
		return missingLocalBuildImageUpdateResultInternal(parts.Tag, int(time.Since(start).Milliseconds())), nil, nil
	}

	registryCtx, registryCancel := s.registryContextInternal(ctx)
	digestResult, digestErr := s.registryService.InspectImageDigest(registryCtx, imageRef, externalCreds)
	registryCancel()
	if digestErr != nil {
		resp := &imageupdate.Response{
			Error:          digestErr.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
		}
		if digestResult != nil {
			resp.AuthMethod = digestResult.AuthMethod
			resp.AuthUsername = digestResult.AuthUsername
			resp.AuthRegistry = digestResult.AuthRegistry
			resp.UsedCredential = digestResult.UsedCredential
		}
		return resp, nil, digestErr
	}

	if ldErr != nil {
		snapshot, ldErr = s.inspectLocalImageSnapshotInternal(ctx, imageRef, composeBuildRefs)
		if ldErr != nil {
			if errdefs.IsNotFound(ldErr) {
				// The ref resolved remotely but was never pulled locally (e.g. a
				// stopped project whose compose pin changed): there is nothing
				// local to compare, so report a distinct not-pulled state rather
				// than a failed check or an available update.
				return &imageupdate.Response{
					UpdateType:     UpdateTypeNotPulled,
					LatestDigest:   digestResult.Digest,
					CheckTime:      time.Now(),
					ResponseTimeMs: int(time.Since(start).Milliseconds()),
					AuthMethod:     digestResult.AuthMethod,
					AuthUsername:   digestResult.AuthUsername,
					AuthRegistry:   digestResult.AuthRegistry,
					UsedCredential: digestResult.UsedCredential,
				}, nil, nil
			}
			return &imageupdate.Response{
				Error:          ldErr.Error(),
				CheckTime:      time.Now(),
				ResponseTimeMs: int(time.Since(start).Milliseconds()),
				AuthMethod:     digestResult.AuthMethod,
				AuthUsername:   digestResult.AuthUsername,
				AuthRegistry:   digestResult.AuthRegistry,
				UsedCredential: digestResult.UsedCredential,
			}, nil, ldErr
		}
	}

	localDigest := snapshot.PrimaryDigest
	hasDigestUpdate := true
	for _, localDig := range snapshot.AllDigests {
		if localDig == digestResult.Digest {
			localDigest = localDig
			hasDigestUpdate = false
			break
		}
	}

	return &imageupdate.Response{
		HasUpdate:      hasDigestUpdate,
		UpdateType:     UpdateTypeDigest,
		CurrentDigest:  localDigest,
		LatestDigest:   digestResult.Digest,
		CheckTime:      time.Now(),
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
		AuthMethod:     digestResult.AuthMethod,
		AuthUsername:   digestResult.AuthUsername,
		AuthRegistry:   digestResult.AuthRegistry,
		UsedCredential: digestResult.UsedCredential,
	}, snapshot, nil
}

// resolveBatchCredentialsInternal returns the caller's credentials for the check, or else the enabled stored
// registry credentials, cached per scan so its tasks decrypt them once while each new scan sees registry edits.
// Failures are not cached. Caller credentials live only in memory, so a check resumed after a restart uses the stored ones.
func (s *ImageUpdateService) resolveBatchCredentialsInternal(ctx context.Context, t flow.Task, input imageCheckInput) []containerregistry.Credential {
	if supplied, ok, _ := s.requestCredentials.Get(input.CredentialsKey); ok && input.CredentialsKey != "" {
		return supplied
	}
	if s.registryService == nil {
		return nil
	}
	scanID := t.ActivityID()
	credentials, _, err := s.credentials.GetWithLoaders(scanID, func([]string) (map[string][]containerregistry.Credential, error) {
		loaded, loadErr := s.loadStoredCredentialsInternal(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		return map[string][]containerregistry.Credential{scanID: loaded}, nil
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to load enabled registries for batch check", "error", err.Error())
		return nil
	}
	return credentials
}

// loadStoredCredentialsInternal decrypts the enabled stored registry credentials.
func (s *ImageUpdateService) loadStoredCredentialsInternal(ctx context.Context) ([]containerregistry.Credential, error) {
	registries, err := s.registryService.GetEnabledRegistries(ctx)
	if err != nil {
		return nil, err
	}

	credentials := make([]containerregistry.Credential, 0, len(registries))
	for _, reg := range registries {
		if strings.TrimSpace(reg.URL) == "" || strings.TrimSpace(reg.Username) == "" || reg.Token == "" {
			continue
		}

		token, decryptErr := crypto.Decrypt(reg.Token)
		if decryptErr != nil {
			slog.DebugContext(ctx, "failed to decrypt registry token for batch check", "registryUrl", reg.URL, "error", decryptErr.Error())
			continue
		}
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		credentials = append(credentials, containerregistry.Credential{
			URL:      reg.URL,
			Username: reg.Username,
			Token:    token,
			Enabled:  reg.Enabled,
		})
	}

	return credentials, nil
}

// recordImageCheckInternal saves a check result and reports it on the activity.
func (s *ImageUpdateService) recordImageCheckInternal(ctx context.Context, activityID, imageRef string, res *imageupdate.Response, snapshot *localImageSnapshot) {
	level, message := imageCheckResultMessageInternal(imageRef, res)
	s.appendImageUpdateActivityMessageInternal(ctx, activityID, level, message, nil, "")
	if err := s.saveUpdateResultWithSnapshotInternal(ctx, imageRef, res, snapshot); err != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", err.Error())
	}
}

// recordDigestPinnedSkipInternal notes a digest-pinned ref as skipped on the
// activity stream and persists its unchanged result.
func (s *ImageUpdateService) recordDigestPinnedSkipInternal(ctx context.Context, activityID, imageRef string, result *imageupdate.Response, progress *int) {
	step := ""
	if progress != nil {
		step = "Skipping image update check"
	}
	s.appendImageUpdateActivityMessageInternal(ctx, activityID, activitytypes.MessageLevelInfo, imageRef+" — digest pinned, skipped", progress, step)
	if err := s.saveUpdateResultWithSnapshotInternal(ctx, imageRef, result, nil); err != nil {
		slog.WarnContext(ctx, "Failed to save digest-pinned update result", "imageRef", imageRef, "error", err.Error())
	}
}

// RegisterWorkflows defines the image-check workflow; call it before the host starts.
func (s *ImageUpdateService) RegisterWorkflows(engine *flow.Engine) error {
	checkWorkflow, err := engine.Define(flow.Definition{
		Name:        "image-check",
		Version:     3,
		Fingerprint: "e508fae9a6d269cc6aaac6375643f761c459e18571aab0efb61d5c0efb94b16d",
		Concurrency: imageCheckConcurrency,
		Timeout:     timeouts.DefaultImageUpdateScan,
		Activity: activity.StartActivityRequest{
			Type:          activitytypes.TypeImageUpdateCheck,
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
			workflow.Step("prepare", engine.Handler(s.prepareImageCheckInternal)),
			workflow.Step("discover", engine.Handler(s.discoverImagesInternal)),
			workflow.ForEach("check", engine.Handler(s.checkImageInternal),
				workflow.WithItemsFrom("discover"),
				workflow.WithInputFrom("prepare"),
				workflow.WithMaxParallel(imageCheckConcurrency),
				workflow.WithFailurePolicy(workflow.TolerateFailures),
				workflow.WithMaxAttempts(imageCheckAttempts),
				workflow.WithRetryBackoff(imageCheckRetryBackoff, imageCheckRetryBackoffMax),
				workflow.WithAttemptTimeout(2*time.Minute)),
			workflow.Step("tags", engine.Handler(s.checkTagPoliciesInternal), workflow.WithInputFrom("discover")),
			workflow.Step("finalize", engine.Handler(s.finalizeImageCheckInternal), workflow.WithInputFrom("discover", "check")),
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

// CheckWorkflow is the image-check workflow, for use as a child step.
func (s *ImageUpdateService) CheckWorkflow() *flow.Workflow { return s.checkWorkflow }

// RunImageCheck checks the requested images and returns the check outcome;
// failed images are reported as failed targets.
func (s *ImageUpdateService) RunImageCheck(ctx context.Context, request imageupdate.CheckRequest) (scheduler.Outcome, error) {
	return s.engine.Run(ctx, s.checkWorkflow, request, activitylib.StartRequest{}, nil)
}

// CheckImages checks the requested images and returns each ref's result. Usable credentials replace the stored
// registries for this check, as they always have.
func (s *ImageUpdateService) CheckImages(ctx context.Context, request imageupdate.CheckRequest, credentials []containerregistry.Credential) (imageupdate.BatchResponse, error) {
	if !request.All && len(request.ImageRefs) == 0 {
		return imageupdate.BatchResponse{}, nil
	}
	input := imageCheckInput{CheckRequest: request, Report: true}
	credentials = slices.DeleteFunc(slices.Clone(credentials), func(credential containerregistry.Credential) bool {
		return !credential.Enabled || strings.TrimSpace(credential.URL) == "" || strings.TrimSpace(credential.Username) == "" || strings.TrimSpace(credential.Token) == ""
	})
	if len(credentials) > 0 {
		input.CredentialsKey = uuid.NewV7().String()
		s.requestCredentials.Set(input.CredentialsKey, credentials)
	}
	var output imageCheckOutput
	outcome, err := s.engine.Run(ctx, s.checkWorkflow, input, activitylib.StartRequest{}, &output)
	if err != nil {
		// The workflow outlives a caller that stopped waiting, so finalize releases the credentials.
		return nil, err
	}
	s.requestCredentials.Delete(input.CredentialsKey)
	if outcome.Status == scheduler.Failed || outcome.Status == scheduler.Canceled {
		return nil, errors.New(cmp.Or(outcome.Message, "image update check failed"))
	}
	results := output.Results
	if results == nil {
		results = imageupdate.BatchResponse{}
	}
	for _, target := range outcome.Targets {
		if target.ResourceType != "image" {
			continue
		}
		if results[target.ID] == nil {
			results[target.ID] = &imageupdate.Response{CheckTime: time.Now()}
		}
		results[target.ID].Error = target.Message
	}
	// Failed images get their entries first, so their containers' tag results still attach.
	tags.AttachContainerUpdates(results, output.ContainerUpdates)
	for _, result := range results {
		result.ActivityID = mo.EmptyableToOption(outcome.ActivityID).ToPointer()
	}
	return results, nil
}

// MonitoredImageRefs keeps the refs whose containers still permit update checks.
func (s *ImageUpdateService) MonitoredImageRefs(ctx context.Context, imageRefs []string) ([]string, error) {
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return nil, err
	}
	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	listed, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("list containers for update-check eligibility: %w", err)
	}
	eligibility := newMonitoringEligibilityInternal(listed.Items)
	return slices.DeleteFunc(slices.Clone(imageRefs), func(imageRef string) bool {
		return !eligibility.imageEligible("", refs.NormalizeImageUpdateRef(imageRef))
	}), nil
}

// prepareImageCheckInternal loads the compose build refs every check task needs, once per scan.
func (s *ImageUpdateService) prepareImageCheckInternal(ctx context.Context, _ flow.Task) (any, error) {
	buildRefs, err := s.composeBuildImageRefsInternal(ctx)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	return slices.Sorted(maps.Keys(buildRefs)), nil
}

func (s *ImageUpdateService) discoverImagesInternal(ctx context.Context, t flow.Task) (any, error) {
	var request imageCheckInput
	if err := t.Payload(&request); err != nil {
		return nil, err
	}
	imageRefs := request.ImageRefs
	if request.All {
		discovered, err := s.getAllImageRefsInternal(ctx, 0)
		if err != nil {
			return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("list image references: %w", err))
		}
		imageRefs = discovered
	}
	s.appendImageUpdateActivityMessageInternal(ctx, t.ActivityID(), activitytypes.MessageLevelInfo, fmt.Sprintf("Checking %d image references", len(imageRefs)), nil, "")
	return s.groupImageRefsInternal(imageRefs), nil
}

// checkImageInternal checks one group of refs. Retryable failures are recorded
// only on the last attempt so earlier attempts never replace a good result.
func (s *ImageUpdateService) checkImageInternal(ctx context.Context, t flow.Task) (any, error) {
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
	imageRef := group[0]
	activityID := t.ActivityID()
	if result, ok := digestPinnedImageUpdateResultInternal(imageRef).Get(); ok {
		result.ActivityID = mo.EmptyableToOption(activityID).ToPointer()
		s.recordDigestPinnedSkipInternal(ctx, activityID, imageRef, result, nil)
		return imageCheckResultInternal{Result: kit.Ternary(input.Report, result, nil)}, nil
	}
	parts := s.parseImageReference(imageRef)
	if parts == nil {
		err := common.Classify(common.ErrValidation, errors.New("invalid image reference format"))
		s.appendImageUpdateActivityMessageInternal(ctx, activityID, activitytypes.MessageLevelError, imageRef+": "+err.Error(), nil, "")
		return nil, err
	}
	if err := s.registryLimiter.Acquire(ctx, parts.Registry); err != nil {
		return nil, err
	}
	defer s.registryLimiter.Release(parts.Registry)
	var buildRefs []string
	if err := t.DecodeOutput("prepare", &buildRefs); err != nil {
		return nil, err
	}
	composeBuildRefs := make(map[string]struct{}, len(buildRefs))
	for _, buildRef := range buildRefs {
		composeBuildRefs[buildRef] = struct{}{}
	}
	res, snapshot, checkErr := s.checkSingleImageInBatchInternal(ctx, s.resolveBatchCredentialsInternal(ctx, t, input), parts, composeBuildRefs)
	// Shutdown hands the task back for redelivery rather than recording it as a failed check.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A reported check answers a waiting caller, so it gets one attempt as the batch endpoint always has.
	if checkErr != nil && flow.Retryable(checkErr) && !input.Report && t.Attempt() < imageCheckAttempts {
		return nil, checkErr
	}
	res.ActivityID = mo.EmptyableToOption(activityID).ToPointer()
	s.recordImageCheckInternal(ctx, activityID, imageRef, res, snapshot)
	if checkErr != nil && input.Report {
		// The waiting caller gets the failed response, auth details included, alongside its error.
		return imageCheckResultInternal{Result: res, Failure: checkErr.Error()}, nil
	}
	if checkErr != nil {
		return nil, checkErr
	}
	return imageCheckResultInternal{Local: res.UpdateType == UpdateTypeLocal, Result: kit.Ternary(input.Report, res, nil)}, nil
}

// checkTagPoliciesInternal checks container tag policies for the non-local refs.
func (s *ImageUpdateService) checkTagPoliciesInternal(ctx context.Context, t flow.Task) (any, error) {
	groups, results, err := imageCheckResultsInternal(t)
	if err != nil {
		return nil, err
	}
	var tagRefs []string
	for index, group := range groups {
		if !results[index].Value.Local {
			tagRefs = append(tagRefs, group...)
		}
	}
	var input imageCheckInput
	if payloadErr := t.Payload(&input); payloadErr != nil {
		return nil, payloadErr
	}
	if input.Report && t.Recovered() {
		return tagCheckOutput{Failures: []scheduler.TargetOutcome{}}, nil
	}
	containerUpdates, err := s.checkContainerTagUpdatesInternal(ctx, tagRefs, s.resolveBatchCredentialsInternal(ctx, t, input))
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	output := tagCheckOutput{Failures: []scheduler.TargetOutcome{}}
	for containerID, update := range containerUpdates {
		if update.Error != "" {
			output.Failures = append(output.Failures, scheduler.TargetOutcome{ResourceType: "container", ID: containerID, Status: scheduler.Failed, Message: update.Error})
		}
	}
	if input.Report {
		output.Updates = containerUpdates
	}
	return output, nil
}

func (s *ImageUpdateService) finalizeImageCheckInternal(ctx context.Context, t flow.Task) (any, error) {
	var request imageCheckInput
	if err := t.Payload(&request); err != nil {
		return nil, err
	}
	// Every check task has run, so the caller's credentials are no longer needed.
	s.requestCredentials.Delete(request.CredentialsKey)
	groups, results, err := imageCheckResultsInternal(t)
	if err != nil {
		return nil, err
	}
	var tagged tagCheckOutput
	if decodeErr := t.DecodeOutput("tags", &tagged); decodeErr != nil {
		return nil, decodeErr
	}
	tagFailures := tagged.Failures
	checked, failed := 0, 0
	var targets []scheduler.TargetOutcome
	for index, group := range groups {
		checked += len(group)
		if results[index].Err == "" {
			continue
		}
		failed += len(group)
		for _, imageRef := range group {
			targets = append(targets, scheduler.TargetOutcome{ResourceType: "image", ID: imageRef, Status: scheduler.Failed, Message: results[index].Err})
		}
	}
	targets = append(targets, tagFailures...)
	// A failed flush is already logged; the records stay pending for the next one.
	_ = s.SendBatchUpdateNotifications(ctx)
	if request.All && (!request.Report || !t.Recovered()) {
		if cleanupErr := s.CleanupOrphanedRecords(ctx); cleanupErr != nil {
			slog.WarnContext(ctx, "failed to cleanup orphaned image update records after check-all", "error", cleanupErr.Error())
		}
	}
	outcome := scheduler.Outcome{
		Status:  scheduler.Succeeded,
		Message: fmt.Sprintf("Image update check completed: %d checked, %d errors", checked, failed),
		Targets: targets,
	}
	if len(tagFailures) > 0 {
		outcome.Message += fmt.Sprintf(", %d tag policy errors", len(tagFailures))
	}
	if len(targets) > 0 {
		outcome.Status = scheduler.Partial
	}
	slog.InfoContext(ctx, "Image update check completed", "totalImages", checked, "failedImages", failed, "failedContainers", len(tagFailures))
	if !request.Report {
		return outcome, nil
	}
	output := imageCheckOutput{Outcome: outcome, Results: imageupdate.BatchResponse{}, ContainerUpdates: tagged.Updates}
	for index, group := range groups {
		if result := results[index].Value.Result; result != nil {
			for _, imageRef := range group {
				output.Results[imageRef] = new(*result)
			}
		}
	}
	return output, nil
}

// imageCheckResultsInternal reads the discovered groups and their check slots.
func imageCheckResultsInternal(t flow.Task) ([][]string, []flow.Result[imageCheckResultInternal], error) {
	var groups [][]string
	if err := t.DecodeOutput("discover", &groups); err != nil {
		return nil, nil, err
	}
	results, err := flow.Results[imageCheckResultInternal](t, "check")
	if err != nil {
		return nil, nil, err
	}
	if len(results) != len(groups) {
		return nil, nil, fmt.Errorf("image check produced %d results for %d groups", len(results), len(groups))
	}
	for index := range results {
		results[index].Err = cmp.Or(results[index].Err, results[index].Value.Failure)
	}
	return groups, results, nil
}

// SendBatchUpdateNotifications delivers pending update notifications. It
// returns an error only when the pending set could not be determined, so a
// caller about to consume the records can stop before their notifications
// are lost; delivery failures leave the records unnotified for the next flush.
func (s *ImageUpdateService) SendBatchUpdateNotifications(ctx context.Context) error {
	if s.notificationService == nil {
		return nil
	}

	// Serialize the query→send→mark sequence so the poll-end flush and the
	// updater consumption-path flush can't both read the same unnotified set
	// and double-send.
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()

	// completeImageUpdateActivityInternal cancels the activity-tracked ctx before
	// we reach here, so detach from that cancellation — otherwise the
	// unnotified-updates query dies with "context canceled" and no notification is
	// ever dispatched. Deliberately not utils.ActivityRuntimeContext: that helper
	// short-circuits (returns ctx unchanged) for app-lifecycle-marked contexts, and
	// every request/scheduler ctx carries that marker via the server BaseContext,
	// making the detach a no-op in production. WithoutCancel keeps all values and
	// the WithTimeout below keeps the flush bounded.
	ctx = context.WithoutCancel(ctx)

	notifCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	unnotifiedUpdates, err := s.GetUnnotifiedUpdates(notifCtx)
	if err == nil && len(unnotifiedUpdates) > 0 {
		// Records for resources that opted out of monitoring since the check, or
		// whose container is gone, stay unnotified: they resurface if monitoring
		// is re-enabled and are otherwise removed by orphan cleanup.
		err = s.filterUnnotifiedByMonitoringInternal(notifCtx, unnotifiedUpdates)
	}
	switch {
	case err != nil:
		slog.WarnContext(ctx, "Failed to get unnotified updates", "error", err.Error())
		return fmt.Errorf("flush pending update notifications: %w", err)
	case len(unnotifiedUpdates) > 0:
		updatesToNotify := make(map[string]*imageupdate.Response)
		imageIDsToMark := make([]string, 0, len(unnotifiedUpdates))

		for imageID, record := range unnotifiedUpdates {
			imageRef := containerUpdateNotificationKeyInternal(record)
			updatesToNotify[imageRef] = responseFromRecordInternal(record)
			imageIDsToMark = append(imageIDsToMark, imageID)
		}

		slog.InfoContext(ctx, "Sending notifications for unnotified updates", "count", len(updatesToNotify))

		delivered, notifErr := s.notificationService.SendBatchImageUpdateNotification(notifCtx, updatesToNotify)
		if notifErr != nil {
			slog.WarnContext(ctx, "Failed to send batch update notification", "error", notifErr.Error())
		}
		// Mark notified when at least one provider delivered — a partial provider
		// failure must not make every healthy provider re-send the same updates on
		// the next poll. Failures remain visible in the delivery history.
		if delivered == 0 {
			slog.DebugContext(ctx, "No providers delivered image update notifications; leaving records unnotified", "count", len(imageIDsToMark))
			return nil
		}
		if markErr := s.MarkUpdatesAsNotified(notifCtx, imageIDsToMark); markErr != nil {
			slog.WarnContext(ctx, "Failed to mark updates as notified", "error", markErr.Error())
		}
	default:
		slog.DebugContext(ctx, "No new updates to notify")
	}
	return nil
}

// DeleteRecordsForImages removes image-level rows (keyed by id) and
// container/project rows (keyed by image_id) for the given images.
func (s *ImageUpdateService) DeleteRecordsForImages(ctx context.Context, imageIDs []string) error {
	if s == nil || s.db == nil || len(imageIDs) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Where("id IN ? OR image_id IN ?", imageIDs, imageIDs).Delete(&ImageUpdateRecord{}).Error; err != nil {
		return fmt.Errorf("failed to delete image update records: %w", err)
	}
	return nil
}

func (s *ImageUpdateService) CleanupOrphanedRecords(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	if s.dockerService == nil {
		return errors.New("docker service unavailable")
	}

	dockerImages, err := s.dockerService.ListImages(ctx)
	if err != nil {
		return err
	}
	dockerImageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		dockerImageIDs = append(dockerImageIDs, img.ID)
	}

	imageScoped := s.db.WithContext(ctx).Where("container_id = ? AND project_id = ?", "", "")
	if len(dockerImageIDs) > 0 {
		imageScoped = imageScoped.Where("id NOT IN ?", dockerImageIDs)
	}
	imageResult := imageScoped.Delete(&ImageUpdateRecord{})
	if imageResult.Error != nil {
		return fmt.Errorf("failed to delete orphaned records: %w", imageResult.Error)
	}

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return err
	}
	containerIDs := make([]string, 0, len(containers))
	for _, cnt := range containers {
		containerIDs = append(containerIDs, cnt.ID)
	}
	containerScoped := s.db.WithContext(ctx).Where("container_id <> ?", "")
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

func (s *ImageUpdateService) GetUpdateSummary(ctx context.Context) (*imageupdate.Summary, error) {
	if s == nil || s.dockerService == nil {
		return nil, errors.New("docker service unavailable")
	}

	dockerImages, err := s.dockerService.ListImages(ctx)
	if err != nil {
		return nil, err
	}

	liveImageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		liveImageIDs = append(liveImageIDs, img.ID)
	}

	return s.getUpdateSummaryForImageIDsInternal(ctx, liveImageIDs)
}

func (s *ImageUpdateService) getUpdateSummaryForImageIDsInternal(ctx context.Context, imageIDs []string) (*imageupdate.Summary, error) {
	summary := &imageupdate.Summary{
		TotalImages: len(imageIDs),
	}

	if s.db == nil || len(imageIDs) == 0 {
		return summary, nil
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
		Scan(&aggregate).Error; err != nil {
		return nil, err
	}

	summary.ImagesWithUpdates = int(aggregate.ImagesWithUpdates)
	summary.DigestUpdates = int(aggregate.DigestUpdates)
	summary.ErrorsCount = int(aggregate.ErrorsCount)

	return summary, nil
}

func (s *ImageUpdateService) checkContainerTagUpdatesInternal(ctx context.Context, imageRefs []string, credentials []containerregistry.Credential) (map[string]*imageupdate.Response, error) {
	results := map[string]*imageupdate.Response{}
	wanted := map[string]bool{}
	for _, imageRef := range imageRefs {
		if normalized := refs.NormalizeImageUpdateRef(imageRef); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return results, nil
	}
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return results, err
	}
	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	listed, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return results, fmt.Errorf("list containers for tag update checks: %w", err)
	}
	return s.tags.Check(ctx, listed.Items, wanted, credentials, s.deleteContainerUpdateRecordsInternal, s.saveContainerTagResultInternal)
}

// staleDigestRecordDeleteChunk bounds the IN list used to clear container
// records whose policy switched to digest tracking.
const staleDigestRecordDeleteChunk = 500

// deleteContainerUpdateRecordsInternal clears the stored results of containers
// whose policy no longer tracks tags, in bounded IN-list chunks.
func (s *ImageUpdateService) deleteContainerUpdateRecordsInternal(ctx context.Context, containerIDs []string) error {
	if s.db == nil {
		return nil
	}
	for chunk := range slices.Chunk(containerIDs, staleDigestRecordDeleteChunk) {
		if err := s.db.WithContext(ctx).Where("container_id IN ?", chunk).Delete(&ImageUpdateRecord{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ImageUpdateService) saveContainerTagResultInternal(ctx context.Context, cnt container.Summary, result *imageupdate.Response) error {
	if s.db == nil {
		return nil
	}
	parsed, err := refs.NormalizeReference(cnt.Image)
	if err != nil {
		return err
	}
	id := "container::" + cnt.ID
	policyKey := imageref.UpdatePolicyKey(cnt.Image, cnt.Labels)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A previous good result must not survive a rate limit under a different policy.
		if deleteStaleUpdatesErr := tx.Where("id = ? AND policy_key <> ?", id, policyKey).Delete(&ImageUpdateRecord{}).Error; deleteStaleUpdatesErr != nil {
			return deleteStaleUpdatesErr
		}
		if savePreparedUpdateResultWithTxErr := savePreparedUpdateResultWithTxInternal(tx, id, parsed.RegistryHost+"/"+parsed.Repository, parsed.Tag, result); savePreparedUpdateResultWithTxErr != nil {
			return savePreparedUpdateResultWithTxErr
		}
		return tx.Model(&ImageUpdateRecord{}).Where("id = ?", id).Updates(map[string]any{"container_id": cnt.ID, "image_id": cnt.ImageID, "policy_key": policyKey}).Error
	})
}

func responseFromRecordInternal(record *ImageUpdateRecord) *imageupdate.Response {
	return &imageupdate.Response{
		HasUpdate:      record.HasUpdate,
		UpdateType:     record.UpdateType,
		CurrentVersion: record.CurrentVersion,
		LatestVersion:  mo.PointerToOption(record.LatestVersion).OrEmpty(),
		CurrentDigest:  mo.PointerToOption(record.CurrentDigest).OrEmpty(),
		LatestDigest:   mo.PointerToOption(record.LatestDigest).OrEmpty(),
		CheckTime:      record.CheckTime,
		ResponseTimeMs: record.ResponseTimeMs,
		Error:          mo.PointerToOption(record.LastError).OrEmpty(),
		AuthMethod:     mo.PointerToOption(record.AuthMethod).OrEmpty(),
		AuthUsername:   mo.PointerToOption(record.AuthUsername).OrEmpty(),
		AuthRegistry:   mo.PointerToOption(record.AuthRegistry).OrEmpty(),
		UsedCredential: record.UsedCredential,
	}
}

func containerUpdateNotificationKeyInternal(record *ImageUpdateRecord) string {
	imageRef := fmt.Sprintf("%s:%s", record.Repository, record.Tag)
	if record.ContainerID != "" {
		return imageRef + " (container " + record.ContainerID + ")"
	}
	return imageRef
}
