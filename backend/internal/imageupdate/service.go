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
	// staleDigestRecordDeleteChunk bounds the IN list used to clear container
	// records whose policy switched to digest tracking.
	staleDigestRecordDeleteChunk = 500
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

// imageCheckResult is a check task's output; failures travel as errors.
type imageCheckResult struct {
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

// monitoringEligibility records, per image ID and normalized
// reference, whether at least one consuming container still permits update
// checks. Only the update-check label is consulted: containers excluded from
// automatic installation keep being monitored.
type monitoringEligibility struct {
	// True when at least one container using the image ID or reference has
	// not opted out; present but false when every such container opted out.
	byImageID   map[string]bool
	byRef       map[string]bool
	byContainer map[string]bool
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

func (s *ImageUpdateService) dockerAPIContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.settingsService == nil {
		return context.WithTimeout(ctx, timeouts.DefaultDockerAPI)
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
}

func (s *ImageUpdateService) registryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.settingsService == nil {
		return context.WithTimeout(ctx, timeouts.DefaultRegistry)
	}
	return context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry))
}

func (s *ImageUpdateService) dockerClient(ctx context.Context) (*client.Client, error) {
	if s.dockerService == nil {
		return nil, errors.New("docker service unavailable")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	return dockerClient, nil
}

func (s *ImageUpdateService) composeBuildImageRefs(ctx context.Context) (map[string]struct{}, error) {
	buildRefs := make(map[string]struct{})
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

	for _, row := range projectRows {
		for _, imageRef := range projects.ParseImageRefsJSON(kit.FromPtr(row.BuildImageRefsJSON)) {
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

// appendImageUpdateActivityMessage appends a check message; a nil
// progress leaves progress to the activity's owner, such as the workflow engine.
func (s *ImageUpdateService) appendImageUpdateActivityMessage(ctx context.Context, activityID string, level activitytypes.MessageLevel, message string, progress *int, step string) {
	if activityID == "" || strings.TrimSpace(message) == "" {
		return
	}
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    cmp.Or(level, activitytypes.MessageLevelInfo),
		Message:  message,
		Progress: progress,
		Step:     step,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append image update activity message", "activityId", activityID, "error", err)
	}
}

func (s *ImageUpdateService) completeImageUpdateActivity(ctx context.Context, activityID string, success bool, message string) {
	if activityID == "" {
		return
	}
	status, errMessage, finalMessage := func() (activitytypes.Status, *string, string) {
		switch {
		case success:
			return activitytypes.StatusSuccess, nil, cmp.Or(message, "Image update check completed")
		case activitylib.CancelledByContext(ctx):
			return activitytypes.StatusCancelled, nil, "Image update check cancelled"
		default:
			return activitytypes.StatusFailed, mo.EmptyableToOption(strings.TrimSpace(message)).ToPointer(), cmp.Or(message, "Image update check completed")
		}
	}()
	if _, err := s.activityService.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, finalMessage, errMessage, "Image update check complete"); err != nil {
		// A lost terminal write strands the activity in running forever, so it
		// must be loud enough to correlate with a stuck activity panel entry.
		slog.ErrorContext(ctx, "failed to complete image update activity", "activityId", activityID, "error", err)
	}
}

func (s *ImageUpdateService) CheckImageUpdate(ctx context.Context, imageRef string) (*imageupdate.Response, error) {
	startTime := time.Now()
	activityID := func() string {
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
	}()
	ctx = s.activityService.Track(ctx, activityID)
	activitylib.AwaitHandlerActivitySlot(ctx, s.activityService, activityID, "0")

	if result, ok := digestPinnedImageUpdateResult(imageRef, activityID, int(time.Since(startTime).Milliseconds())).Get(); ok {
		s.recordDigestPinnedSkip(ctx, activityID, imageRef, result, new(100))
		if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, "", imageRef, user.SystemUser.ID, user.SystemUser.Username, "0", database.JSON{
			"action":         "check_update",
			"imageRef":       imageRef,
			"hasUpdate":      false,
			"updateType":     UpdateTypeDigest,
			"currentDigest":  result.CurrentDigest,
			"latestDigest":   result.LatestDigest,
			"responseTimeMs": result.ResponseTimeMs,
			"skippedReason":  "digest_pinned",
		}); logErr != nil {
			slog.WarnContext(ctx, "Failed to log digest-pinned image update check event", "imageRef", imageRef, "error", logErr.Error())
		}
		s.completeImageUpdateActivity(ctx, activityID, true, "Image update check skipped")
		return result, nil
	}

	s.appendImageUpdateActivityMessage(ctx, activityID, activitytypes.MessageLevelInfo, "Checking "+imageRef, new(20), "Checking remote digest")

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		result := &imageupdate.Response{
			Error:          "Invalid image reference format",
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}
		s.completeImageUpdateActivity(ctx, activityID, false, result.Error)
		return result, nil
	}

	digestResult, snapshot, err := func() (*imageupdate.Response, *localImageSnapshot, error) {
		composeBuildRefs, composeErr := s.composeBuildImageRefs(ctx)
		if composeErr != nil {
			return &imageupdate.Response{Error: composeErr.Error(), CheckTime: time.Now()}, nil, composeErr
		}
		return s.checkSingleImageInBatch(ctx, nil, parts, composeBuildRefs, activityID, startTime)
	}()
	containerUpdates, tagErr := s.checkContainerTagUpdates(ctx, kit.Ternary(digestResult.UpdateType == UpdateTypeLocal, nil, []string{imageRef}), nil)
	if tagErr != nil {
		s.completeImageUpdateActivity(ctx, activityID, false, tagErr.Error())
		return digestResult, tagErr
	}
	if err != nil {
		result := &imageupdate.Response{
			Error:          err.Error(),
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(startTime).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}
		if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, "", imageRef, user.SystemUser.ID, user.SystemUser.Username, "0", database.JSON{
			"action":    "check_update",
			"imageRef":  imageRef,
			"error":     err.Error(),
			"checkType": "digest",
		}); logErr != nil {
			slog.WarnContext(ctx, "Failed to log image update check error event", "imageRef", imageRef, "error", logErr.Error())
		}
		if saveErr := s.saveUpdateResultWithSnapshot(ctx, imageRef, result, snapshot); saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
		}
		attached := tags.AttachContainerUpdates(map[string]*imageupdate.Response{imageRef: result}, containerUpdates)[imageRef]
		s.completeImageUpdateActivity(ctx, activityID, false, attached.Error)
		if attached.HasUpdate {
			// A failed flush is already logged; the records stay pending for the next one.
			_ = s.SendBatchUpdateNotifications(ctx)
			return attached, nil
		}
		return attached, err
	}

	if digestResult.UpdateType == UpdateTypeLocal {
		s.appendImageUpdateActivityMessage(ctx, activityID, activitytypes.MessageLevelInfo, imageRef+" — local build, registry check skipped", new(100), "Skipping image update check")
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, "", imageRef, user.SystemUser.ID, user.SystemUser.Username, "0", database.JSON{
		"action":         "check_update",
		"imageRef":       imageRef,
		"hasUpdate":      digestResult.HasUpdate,
		"updateType":     digestResult.UpdateType,
		"currentDigest":  digestResult.CurrentDigest,
		"latestDigest":   digestResult.LatestDigest,
		"responseTimeMs": digestResult.ResponseTimeMs,
	}); logErr != nil {
		slog.WarnContext(ctx, "Failed to log image update check event", "imageRef", imageRef, "error", logErr.Error())
	}
	if saveErr := s.saveUpdateResultWithSnapshot(ctx, imageRef, digestResult, snapshot); saveErr != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", imageRef, "error", saveErr.Error())
	}

	// Mark the record notified once at least one provider delivered: re-sending a partial
	// failure on every poll would duplicate it forever; only delivered == 0 is retried.
	if digestResult.HasUpdate {
		delivered, notifErr := s.notificationService.SendImageUpdateNotification(ctx, imageRef, digestResult, notifications.NotificationEventImageUpdate)
		if notifErr != nil {
			slog.WarnContext(ctx, "Failed to send update notification", "imageRef", imageRef, "error", notifErr.Error())
		}
		if delivered > 0 {
			// Prefer the snapshot's image ID: it is the same key saveUpdateResultWithSnapshot stored the record under.
			imageID, imageIDErr := func() (string, error) {
				if kit.FromPtr(snapshot).ImageID != "" {
					return snapshot.ImageID, nil
				}
				return s.getImageIDByRef(ctx, imageRef)
			}()
			switch {
			case imageIDErr != nil:
				slog.WarnContext(ctx, "Failed to resolve image ID to mark notified", "imageRef", imageRef, "error", imageIDErr.Error())
			case imageID != "":
				if markErr := s.MarkUpdatesAsNotified(ctx, []string{imageID}); markErr != nil {
					slog.WarnContext(ctx, "Failed to mark update as notified", "imageRef", imageRef, "error", markErr.Error())
				}
			}
		}
	}

	result := tags.AttachContainerUpdates(map[string]*imageupdate.Response{imageRef: digestResult}, containerUpdates)[imageRef]
	if len(containerUpdates) > 0 {
		// A failed flush is already logged; the records stay pending for the next one.
		_ = s.SendBatchUpdateNotifications(ctx)
	}

	s.completeImageUpdateActivity(ctx, activityID, true, kit.Ternary(result.HasUpdate, "Image update available", "Image update check completed"))
	return result, nil
}

func digestPinnedImageUpdateResult(imageRef, activityID string, responseTimeMs int) mo.Option[*imageupdate.Response] {
	pinnedDigest, ok := digest.FromReferenceSuffix(strings.TrimSpace(imageRef))
	if !ok {
		return mo.None[*imageupdate.Response]()
	}

	tag := func() string {
		if named, err := reference.ParseNormalizedNamed(strings.TrimSpace(imageRef)); err == nil {
			if tagged, localOk := named.(reference.NamedTagged); localOk {
				return tagged.Tag()
			}
		}
		return "latest"
	}()

	return mo.Some(&imageupdate.Response{
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
		registryauth.NormalizeRegistryForComparison(reference.Domain(named)) == imageref.LocalBuildRegistry {
		return true
	}
	_, ok := composeBuildRefs[refs.NormalizeImageUpdateRef(imageRef)]
	return ok
}

func (s *ImageUpdateService) parseImageReference(imageRef string) *ImageParts {
	// Use the official Docker reference parser to handle all edge cases
	if named, err := reference.ParseNormalizedNamed(imageRef); err == nil {
		return &ImageParts{
			Registry:   registryauth.NormalizeRegistryForComparison(reference.Domain(named)),
			Repository: reference.Path(named),
			// Extract tag or default to latest
			Tag: func() string {
				if tagged, ok := named.(reference.NamedTagged); ok {
					return tagged.Tag()
				}
				return "latest"
			}(),
		}
	}

	// Fallback to manual parsing if the official parser fails
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
				Registry:   registryauth.NormalizeRegistryForComparison(parts[0]),
				Repository: strings.Join(parts[1:], "/"),
				Tag:        "latest",
			}
		}
		return &ImageParts{
			Registry:   registryauth.NormalizeRegistryForComparison("docker.io"),
			Repository: kit.Ternary(len(parts) == 1, "library/"+parts[0], digestParts[0]),
			Tag:        "latest",
		}
	}
	parts := strings.Split(imageRef, "/")
	switch {
	case len(parts) == 1:
		repository, tag := splitTag(parts[0])
		return &ImageParts{
			Registry:   registryauth.NormalizeRegistryForComparison("docker.io"),
			Repository: "library/" + repository,
			Tag:        tag,
		}
	case strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":"):
		repository, tag := splitTag(strings.Join(parts[1:], "/"))
		return &ImageParts{
			Registry:   registryauth.NormalizeRegistryForComparison(parts[0]),
			Repository: repository,
			Tag:        tag,
		}
	default:
		repository, tag := splitTag(imageRef)
		return &ImageParts{
			Registry:   registryauth.NormalizeRegistryForComparison("docker.io"),
			Repository: repository,
			Tag:        tag,
		}
	}
}

func (s *ImageUpdateService) getImageRefByID(ctx context.Context, imageID string) (string, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return "", err
	}

	imageID = strings.TrimPrefix(imageID, "sha256:")

	if imageRef, ok := func() (string, bool) {
		apiCtx, cancel := s.dockerAPIContext(ctx)
		defer cancel()

		inspectResponse, inspectErr := dockerClient.ImageInspect(apiCtx, imageID)
		if inspectErr != nil {
			return "", false
		}
		for _, tag := range inspectResponse.RepoTags {
			if tag != "<none>:<none>" {
				return tag, true
			}
		}
		for _, digestValue := range inspectResponse.RepoDigests {
			if digestValue != "<none>@<none>" {
				if repo, _, found := strings.Cut(digestValue, "@"); found {
					return repo + ":latest", true
				}
			}
		}
		return "", false
	}(); ok {
		return imageRef, nil
	}

	// Fallback: if the image was pruned, look up the image reference from
	// running containers that were started from this image ID.
	if imageRef, ok := func() (string, bool) {
		apiCtx, cancel := s.dockerAPIContext(ctx)
		defer cancel()

		containers, listErr := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
		if listErr != nil {
			return "", false
		}
		for _, c := range containers.Items {
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

	return "", fmt.Errorf("image not found: no local image or running container found for %s", imageID)
}

func (s *ImageUpdateService) getAllImageRefs(ctx context.Context, limit int) ([]string, error) {
	imageCtx, cancelImage := s.dockerAPIContext(ctx)
	images, err := s.dockerService.ListImages(imageCtx)
	cancelImage()
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker images: %w", err)
	}

	containerCtx, cancelContainers := s.dockerAPIContext(ctx)
	containers, err := s.dockerService.ListContainers(containerCtx)
	cancelContainers()
	if err != nil {
		slog.WarnContext(ctx, "failed to list Docker containers; continuing without update-check opt-out filtering", "error", err.Error())
		return filterImageSummariesByContainerOptOut(images, nil, limit), nil
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

// imageEligible reports whether an image may be checked: unused images are
// eligible, used images need one consumer that has not opted out.
func (e monitoringEligibility) imageEligible(imageID, normalizedRef string) bool {
	eligibleByID, usedByID := e.byImageID[strings.TrimSpace(imageID)]
	eligibleRef, usedByRef := e.byRef[normalizedRef]
	return (!usedByID && !usedByRef) || eligibleByID || eligibleRef
}

func filterImageSummariesByContainerOptOut(images []image.Summary, containers []container.Summary, limit int) []string {
	eligibility := newMonitoringEligibility(containers)

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

func (s *ImageUpdateService) inspectLocalImageSnapshot(ctx context.Context, imageRef string, composeBuildRefs map[string]struct{}) (*localImageSnapshot, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, err
	}

	apiCtx, cancel := s.dockerAPIContext(ctx)
	defer cancel()

	inspectResponse, err := dockerClient.ImageInspect(apiCtx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}

	var allDigests, repositoryDigests []string
	requested, referenceErr := refs.NormalizeReference(imageRef)

	// Extract all digests from RepoDigests
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

	repo, tag := extractRepoAndTagFromImage(inspectResponse.InspectResponse)

	return &localImageSnapshot{
		ImageID:    inspectResponse.ID,
		Repository: repo,
		Tag:        tagWithFallback(tag, s.parseImageReference(imageRef)),
		// Use the first repository digest, else the first digest; fallback to image ID if no repo digests available
		PrimaryDigest:     slices.Concat(repositoryDigests, allDigests, []string{inspectResponse.ID})[0],
		AllDigests:        kit.Ternary(len(allDigests) > 0, allDigests, []string{inspectResponse.ID}),
		RepositoryDigests: repositoryDigests,
		IsLocalBuild:      len(allDigests) == 0 || isLocalBuildImageRef(imageRef, composeBuildRefs),
	}, nil
}

func (s *ImageUpdateService) CheckImageUpdateByID(ctx context.Context, imageID string) (*imageupdate.Response, error) {
	imageRef, err := s.getImageRefByID(ctx, imageID)
	if err != nil {
		if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageScan, imageID, "", user.SystemUser.ID, user.SystemUser.Username, "0", database.JSON{
			"action":  "check_update_by_id",
			"imageID": imageID,
			"error":   err.Error(),
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
		if saveErr := s.saveUpdateResultByID(ctx, imageID, result, s.parseImageReference(imageRef)); saveErr != nil {
			slog.WarnContext(ctx, "Failed to save update result by ID", "imageId", imageID, "error", saveErr.Error())
		}
	}
	return result, nil
}

func (s *ImageUpdateService) saveUpdateResultWithSnapshot(ctx context.Context, imageRef string, result *imageupdate.Response, snapshot *localImageSnapshot) error {
	if kit.FromPtr(snapshot).ImageID != "" {
		return s.savePreparedUpdateResult(ctx, snapshot.ImageID, snapshot.Repository, snapshot.Tag, result)
	}

	parts := s.parseImageReference(imageRef)
	if parts == nil {
		return errors.New("invalid image reference")
	}
	imageID, err := s.getImageIDByRef(ctx, imageRef)
	if err != nil {
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
		slog.DebugContext(ctx, "Saving image update result with synthetic ref ID",
			"imageRef", imageRef,
			"error", err.Error(),
			"repository", repository,
			"tag", parts.Tag,
			"syntheticId", syntheticID)
		// Persist registry results even when the local image no longer exists. This keeps
		// project/image update status available for pruned images using a ref-scoped record.
		return s.savePreparedUpdateResult(ctx, syntheticID, repository, parts.Tag, result)
	}

	return s.saveUpdateResultByID(ctx, imageID, result, parts)
}

func extractRepoAndTagFromImage(dockerImage image.InspectResponse) (repo, tag string) {
	if len(dockerImage.RepoTags) > 0 && dockerImage.RepoTags[0] != "<none>:<none>" {
		if named, err := reference.ParseNormalizedNamed(dockerImage.RepoTags[0]); err == nil {
			if tagged, ok := named.(reference.NamedTagged); ok {
				return reference.FamiliarName(named), tagged.Tag()
			}
			return reference.FamiliarName(named), "latest"
		}

		parts := strings.SplitN(dockerImage.RepoTags[0], ":", 2)
		if len(parts) > 1 {
			return parts[0], parts[1]
		}
		return parts[0], "latest"
	}

	for _, rd := range dockerImage.RepoDigests {
		if rd == "<none>@<none>" {
			continue
		}
		if repoCandidate, _, found := strings.CutLast(rd, "@"); found && repoCandidate != "" {
			return repoCandidate, "<none>"
		}
	}

	return "<none>", "<none>"
}

func buildImageUpdateRecord(imageID, repo, tag string, result *imageupdate.Response, notificationSent bool) *ImageUpdateRecord {
	return &ImageUpdateRecord{ //exhaustruct:enforce
		BaseModel: database.BaseModel{
			ID:        "",
			CreatedAt: time.Time{},
			UpdatedAt: nil,
		},
		ProjectID:        "",
		ServiceName:      "",
		PolicyKey:        "",
		ContainerID:      "",
		ImageID:          "",
		CheckTime:        result.CheckTime,
		LatestVersion:    mo.EmptyableToOption(result.LatestVersion).ToPointer(),
		CurrentDigest:    mo.EmptyableToOption(result.CurrentDigest).ToPointer(),
		LatestDigest:     mo.EmptyableToOption(result.LatestDigest).ToPointer(),
		LastError:        mo.EmptyableToOption(result.Error).ToPointer(),
		AuthMethod:       mo.EmptyableToOption(result.AuthMethod).ToPointer(),
		AuthUsername:     mo.EmptyableToOption(result.AuthUsername).ToPointer(),
		AuthRegistry:     mo.EmptyableToOption(result.AuthRegistry).ToPointer(),
		ID:               imageID,
		Repository:       repo,
		Tag:              tag,
		UpdateType:       result.UpdateType,
		CurrentVersion:   cmp.Or(result.CurrentVersion, tag),
		ResponseTimeMs:   result.ResponseTimeMs,
		HasUpdate:        result.HasUpdate,
		UsedCredential:   result.UsedCredential,
		NotificationSent: notificationSent,
	}
}

func savePreparedUpdateResultWithTx(tx *gorm.DB, imageID, repo, tag string, result *imageupdate.Response) error {
	// Check if there's an existing record to compare state changes
	var existingRecord ImageUpdateRecord
	hasExisting := tx.Where("id = ?", imageID).First(&existingRecord).Error == nil

	// A registry rate limit (429) is transient: keep the previous good result
	// instead of clobbering it with an error record
	if hasExisting &&
		strings.TrimSpace(result.Error) != "" &&
		registry.IsRateLimitErrorString(result.Error) &&
		strings.TrimSpace(kit.FromPtr(existingRecord.LastError)) == "" {
		slog.DebugContext(tx.Statement.Context, "Preserving previous image update result; check hit a registry rate limit",
			"imageId", imageID, "repository", repo, "tag", tag, "error", result.Error)
		return nil
	}

	// Reset notification_sent if the update state changed in any way; keep the existing
	// value if nothing changed. New records start with notification_sent = false.
	return tx.Save(buildImageUpdateRecord(imageID, repo, tag, result,
		hasExisting &&
			existingRecord.HasUpdate == result.HasUpdate &&
			(!result.HasUpdate || (kit.FromPtr(existingRecord.LatestDigest) == result.LatestDigest && kit.FromPtr(existingRecord.LatestVersion) == result.LatestVersion)) &&
			existingRecord.NotificationSent,
	)).Error
}

func (s *ImageUpdateService) saveUpdateResultByID(ctx context.Context, imageID string, result *imageupdate.Response, fallback *ImageParts) error {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return err
	}

	apiCtx, cancel := s.dockerAPIContext(ctx)
	defer cancel()

	dockerImage, err := dockerClient.ImageInspect(apiCtx, imageID)
	if err != nil {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	repo, tag := extractRepoAndTagFromImage(dockerImage.InspectResponse)
	return s.savePreparedUpdateResult(ctx, imageID, repo, tagWithFallback(tag, fallback), result)
}

func tagWithFallback(tag string, fallback *ImageParts) string {
	if tag == "<none>" && strings.TrimSpace(kit.FromPtr(fallback).Tag) != "" {
		return fallback.Tag
	}
	return tag
}

func (s *ImageUpdateService) savePreparedUpdateResult(ctx context.Context, imageID, repo, tag string, result *imageupdate.Response) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return savePreparedUpdateResultWithTx(tx, imageID, repo, tag, result)
	})
}

func (s *ImageUpdateService) getImageIDByRef(ctx context.Context, imageRef string) (string, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return "", err
	}

	apiCtx, cancel := s.dockerAPIContext(ctx)
	defer cancel()

	inspectResponse, err := dockerClient.ImageInspect(apiCtx, imageRef)
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	return inspectResponse.ID, nil
}

func (s *ImageUpdateService) MarkImageRefUpToDateAfterPull(ctx context.Context, imageRef string) error {
	snapshot, err := s.inspectLocalImageSnapshot(ctx, imageRef, nil)
	if err != nil {
		return fmt.Errorf("inspect pulled image: %w", err)
	}

	checkTime := time.Now().UTC()
	_, tag, repositoryCandidates, hasLookup := imageref.ParseUpdateLookup(imageRef)
	// projectChecks are the digest previews a pull of imageRef satisfies. Previews that were disabled,
	// local, or not pulled carry no digest result and are left for the next project check.
	projectChecks, confirmedDigest, err := func() ([]ImageUpdateRecord, string, error) {
		if len(repositoryCandidates) == 0 || len(snapshot.RepositoryDigests) == 0 {
			return nil, "", nil
		}
		var records []ImageUpdateRecord
		if findErr := s.db.WithContext(ctx).
			Where("project_id <> '' AND update_type = ? AND tag = ? AND repository IN ?", UpdateTypeDigest, tag, slices.Collect(maps.Keys(repositoryCandidates))).
			Where("latest_version IS NULL OR latest_version = '' OR latest_version = tag").
			Where("last_error IS NULL OR last_error = ''").Find(&records).Error; findErr != nil {
			return nil, "", fmt.Errorf("load project update checks after pull: %w", findErr)
		}
		digestMismatch := func(record ImageUpdateRecord) bool {
			return kit.FromPtr(record.LatestDigest) != "" && !slices.Contains(snapshot.RepositoryDigests, kit.FromPtr(record.LatestDigest))
		}
		if !slices.ContainsFunc(records, digestMismatch) {
			return records, "", nil
		}

		// Digests have no ordering. Confirm a moved tag without using the digest cache.
		// A registry failure leaves the differing previews untouched rather than
		// blocking the reconciliation of the pulled image state.
		if s.registryService != nil {
			registryCtx, cancel := s.registryContext(ctx)
			defer cancel()
			latest, inspectErr := s.registryService.InspectImageDigest(registryCtx, imageRef, nil)
			if inspectErr != nil {
				slog.WarnContext(ctx, "Failed to verify pulled image against project previews", "imageRef", imageRef, "error", inspectErr.Error())
			} else if slices.Contains(snapshot.RepositoryDigests, kit.FromPtr(latest).Digest) {
				return records, latest.Digest, nil
			}
		}
		return slices.DeleteFunc(records, digestMismatch), "", nil
	}()
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if hasLookup && len(repositoryCandidates) > 0 {
			// Only clear synthetic ref:: records, not real sha256 image ID records.
			// Clearing sha256 records would incorrectly mark containers that are still
			// running the old image as up-to-date (see: #2453).
			if clearStaleUpdatesErr := tx.Model(&ImageUpdateRecord{}).
				Where("id LIKE 'ref::%' AND tag = ? AND repository IN ?", tag, slices.Collect(maps.Keys(repositoryCandidates))).
				Update("has_update", false).Error; clearStaleUpdatesErr != nil {
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

		if savePreparedUpdateResultWithTxErr := savePreparedUpdateResultWithTx(tx, snapshot.ImageID, snapshot.Repository, snapshot.Tag, &imageupdate.Response{
			UpdateType:     UpdateTypeDigest,
			CurrentVersion: snapshot.Tag,
			LatestVersion:  snapshot.Tag,
			CurrentDigest:  snapshot.PrimaryDigest,
			LatestDigest:   snapshot.PrimaryDigest,
			CheckTime:      checkTime,
		}); savePreparedUpdateResultWithTxErr != nil {
			return fmt.Errorf("save pulled image update state: %w", savePreparedUpdateResultWithTxErr)
		}

		return nil
	})
}

func (s *ImageUpdateService) StoredUpdateByImageID(ctx context.Context, imageID string) (*ImageUpdateRecord, bool, error) {
	if s == nil || strings.TrimSpace(imageID) == "" {
		return nil, false, nil
	}

	var record ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(imageID)).First(&record).Error; err != nil {
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

// groupImageRefs dedupes refs naming the same registry/repo:tag into one
// check; the first ref of a group is canonical. Digest-pinned and invalid refs
// each form their own group.
func (s *ImageUpdateService) groupImageRefs(imageRefs []string) [][]string {
	var groups [][]string
	indexByNormalizedRef := make(map[string]int)
	for _, imageRef := range imageRefs {
		parts := s.parseImageReference(imageRef)
		if parts == nil || digestPinnedImageUpdateResult(imageRef, "", 0).IsPresent() {
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

func (s *ImageUpdateService) checkSingleImageInBatch(
	ctx context.Context,
	externalCreds []containerregistry.Credential,
	parts *ImageParts,
	composeBuildRefs map[string]struct{},
	activityID string,
	start time.Time,
) (*imageupdate.Response, *localImageSnapshot, error) {
	imageRef := fmt.Sprintf("%s/%s:%s", parts.Registry, parts.Repository, parts.Tag)
	snapshot, ldErr := s.inspectLocalImageSnapshot(ctx, imageRef, composeBuildRefs)
	if ldErr == nil && snapshot.IsLocalBuild {
		return &imageupdate.Response{
			UpdateType:     UpdateTypeLocal,
			CurrentVersion: snapshot.Tag,
			CurrentDigest:  snapshot.PrimaryDigest,
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}, snapshot, nil
	}
	// A compose build ref whose image is not present locally has no registry to
	// check against, so the registry lookup is skipped entirely.
	if ldErr != nil && errdefs.IsNotFound(ldErr) && isLocalBuildImageRef(imageRef, composeBuildRefs) {
		return &imageupdate.Response{
			UpdateType:     UpdateTypeLocal,
			CurrentVersion: parts.Tag,
			CheckTime:      time.Now(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		}, nil, nil
	}

	registryCtx, registryCancel := s.registryContext(ctx)
	digestResult, digestErr := s.registryService.InspectImageDigest(registryCtx, imageRef, externalCreds)
	registryCancel()
	if digestErr != nil {
		// digestResult may contain auth metadata even on error
		return &imageupdate.Response{
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
				// The ref resolved remotely but was never pulled locally (e.g. a
				// stopped project whose compose pin changed): there is nothing
				// local to compare, so report a distinct not-pulled state rather
				// than a failed check or an available update.
				return &imageupdate.Response{
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
			return &imageupdate.Response{
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

	// Deliberately index-level: local RepoDigests and the registry digest are both the index digest a tag resolves to,
	// so this answers "would a pull fetch something new". Do not align it to platform-level digests.
	slog.DebugContext(ctx, "digest comparison",
		"imageRef", imageRef,
		"primaryLocalDigest", kit.Ternary(slices.Contains(snapshot.AllDigests, digestResult.Digest), digestResult.Digest, snapshot.PrimaryDigest),
		"allLocalDigests", snapshot.AllDigests,
		"remoteDigest", digestResult.Digest,
		"hasUpdate", !slices.Contains(snapshot.AllDigests, digestResult.Digest))

	return &imageupdate.Response{
		HasUpdate:      !slices.Contains(snapshot.AllDigests, digestResult.Digest),
		UpdateType:     UpdateTypeDigest,
		CurrentDigest:  kit.Ternary(slices.Contains(snapshot.AllDigests, digestResult.Digest), digestResult.Digest, snapshot.PrimaryDigest),
		LatestDigest:   digestResult.Digest,
		CheckTime:      time.Now(),
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
		ActivityID:     mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		AuthMethod:     digestResult.AuthMethod,
		AuthUsername:   digestResult.AuthUsername,
		AuthRegistry:   digestResult.AuthRegistry,
		UsedCredential: digestResult.UsedCredential,
	}, snapshot, nil
}

// resolveBatchCredentials returns the caller's credentials for the check, or else the enabled stored
// registry credentials, cached per scan so its tasks decrypt them once while each new scan sees registry edits.
// Failures are not cached. Caller credentials live only in memory, so a check resumed after a restart uses the stored ones.
func (s *ImageUpdateService) resolveBatchCredentials(ctx context.Context, t flow.Task, input imageCheckInput) []containerregistry.Credential {
	if supplied, ok, _ := s.requestCredentials.Get(input.CredentialsKey); ok && input.CredentialsKey != "" {
		return supplied
	}
	if s.registryService == nil {
		return nil
	}
	credentials, _, err := s.credentials.GetWithLoaders(t.ActivityID(), func([]string) (map[string][]containerregistry.Credential, error) {
		// Decrypt the enabled stored registry credentials.
		registries, loadErr := s.registryService.GetEnabledRegistries(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		loaded := make([]containerregistry.Credential, 0, len(registries))
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

			loaded = append(loaded, containerregistry.Credential{
				URL:      reg.URL,
				Username: reg.Username,
				Token:    strings.TrimSpace(token),
				Enabled:  reg.Enabled,
			})
		}
		return map[string][]containerregistry.Credential{t.ActivityID(): loaded}, nil
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to load enabled registries for batch check", "error", err.Error())
		return nil
	}
	return credentials
}

// recordDigestPinnedSkip notes a digest-pinned ref as skipped on the
// activity stream and persists its unchanged result.
func (s *ImageUpdateService) recordDigestPinnedSkip(ctx context.Context, activityID, imageRef string, result *imageupdate.Response, progress *int) {
	s.appendImageUpdateActivityMessage(ctx, activityID, activitytypes.MessageLevelInfo, imageRef+" — digest pinned, skipped", progress, kit.Ternary(progress != nil, "Skipping image update check", ""))
	if err := s.saveUpdateResultWithSnapshot(ctx, imageRef, result, nil); err != nil {
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
	credentials = slices.DeleteFunc(slices.Clone(credentials), func(credential containerregistry.Credential) bool {
		return !credential.Enabled || strings.TrimSpace(credential.URL) == "" || strings.TrimSpace(credential.Username) == "" || strings.TrimSpace(credential.Token) == ""
	})
	input := imageCheckInput{
		CheckRequest:   request,
		Report:         true,
		CredentialsKey: kit.Ternary(len(credentials) > 0, uuid.NewV7().String(), ""),
	}
	if input.CredentialsKey != "" {
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
	failures := map[string]string{}
	for _, target := range outcome.Targets {
		if target.ResourceType == "image" {
			failures[target.ID] = target.Message
		}
	}
	results := make(imageupdate.BatchResponse, len(output.Results)+len(failures))
	for _, imageRef := range slices.Concat(slices.Collect(maps.Keys(output.Results)), slices.Collect(maps.Keys(failures))) {
		result := cmp.Or(output.Results[imageRef], &imageupdate.Response{CheckTime: time.Now()})
		message, failed := failures[imageRef]
		results[imageRef] = &imageupdate.Response{ //exhaustruct:enforce
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
			Error:            kit.Ternary(failed, message, result.Error),
			AuthMethod:       result.AuthMethod,
			AuthUsername:     result.AuthUsername,
			AuthRegistry:     result.AuthRegistry,
			ResponseTimeMs:   result.ResponseTimeMs,
			HasUpdate:        result.HasUpdate,
			UsedCredential:   result.UsedCredential,
		}
	}
	// Failed images get their entries first, so their containers' tag results still attach.
	return tags.AttachContainerUpdates(results, output.ContainerUpdates), nil
}

// MonitoredImageRefs keeps the refs whose containers still permit update checks.
func (s *ImageUpdateService) MonitoredImageRefs(ctx context.Context, imageRefs []string) ([]string, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, err
	}
	apiCtx, cancel := s.dockerAPIContext(ctx)
	listed, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("list containers for update-check eligibility: %w", err)
	}
	eligibility := newMonitoringEligibility(listed.Items)
	return slices.DeleteFunc(slices.Clone(imageRefs), func(imageRef string) bool {
		return !eligibility.imageEligible("", refs.NormalizeImageUpdateRef(imageRef))
	}), nil
}

// prepareImageCheck loads the compose build refs every check task needs, once per scan.
func (s *ImageUpdateService) prepareImageCheck(ctx context.Context, _ flow.Task) (any, error) {
	buildRefs, err := s.composeBuildImageRefs(ctx)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	return slices.Sorted(maps.Keys(buildRefs)), nil
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
	s.appendImageUpdateActivityMessage(ctx, t.ActivityID(), activitytypes.MessageLevelInfo, fmt.Sprintf("Checking %d image references", len(imageRefs)), nil, "")
	return s.groupImageRefs(imageRefs), nil
}

// checkImage checks one group of refs. Retryable failures are recorded
// only on the last attempt so earlier attempts never replace a good result.
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
	if result, ok := digestPinnedImageUpdateResult(group[0], t.ActivityID(), 0).Get(); ok {
		s.recordDigestPinnedSkip(ctx, t.ActivityID(), group[0], result, nil)
		return imageCheckResult{Result: kit.Ternary(input.Report, result, nil)}, nil
	}
	parts := s.parseImageReference(group[0])
	if parts == nil {
		err := common.Classify(common.ErrValidation, errors.New("invalid image reference format"))
		s.appendImageUpdateActivityMessage(ctx, t.ActivityID(), activitytypes.MessageLevelError, group[0]+": "+err.Error(), nil, "")
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
	res, snapshot, checkErr := s.checkSingleImageInBatch(ctx, s.resolveBatchCredentials(ctx, t, input), parts, composeBuildRefs, t.ActivityID(), time.Now())
	// Shutdown hands the task back for redelivery rather than recording it as a failed check.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A reported check answers a waiting caller, so it gets one attempt as the batch endpoint always has.
	if checkErr != nil && flow.Retryable(checkErr) && !input.Report && t.Attempt() < imageCheckAttempts {
		return nil, checkErr
	}
	// Save the result and report it on the activity: errors become ERROR, available updates
	// become SUCCESS, and up-to-date images stay INFO.
	level, message := func() (activitytypes.MessageLevel, string) {
		switch {
		case strings.TrimSpace(res.Error) != "":
			return activitytypes.MessageLevelError, fmt.Sprintf("%s: %s", group[0], strings.TrimSpace(res.Error))
		case res.UpdateType == UpdateTypeLocal:
			return activitytypes.MessageLevelInfo, group[0] + " — local build, registry check skipped"
		case res.UpdateType == UpdateTypeNotPulled:
			return activitytypes.MessageLevelInfo, group[0] + " — not pulled locally, no local digest to compare"
		case res.HasUpdate:
			return activitytypes.MessageLevelSuccess, group[0] + " — update available"
		default:
			return activitytypes.MessageLevelInfo, group[0] + " — up to date"
		}
	}()
	s.appendImageUpdateActivityMessage(ctx, t.ActivityID(), level, message, nil, "")
	if err := s.saveUpdateResultWithSnapshot(ctx, group[0], res, snapshot); err != nil {
		slog.WarnContext(ctx, "Failed to save update result", "imageRef", group[0], "error", err.Error())
	}
	if checkErr != nil && input.Report {
		// The waiting caller gets the failed response, auth details included, alongside its error.
		return imageCheckResult{Result: res, Failure: checkErr.Error()}, nil
	}
	if checkErr != nil {
		return nil, checkErr
	}
	return imageCheckResult{Local: res.UpdateType == UpdateTypeLocal, Result: kit.Ternary(input.Report, res, nil)}, nil
}

// checkTagPolicies checks container tag policies for the non-local refs.
func (s *ImageUpdateService) checkTagPolicies(ctx context.Context, t flow.Task) (any, error) {
	groups, results, err := imageCheckResults(t)
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
	containerUpdates, err := s.checkContainerTagUpdates(ctx, tagRefs, s.resolveBatchCredentials(ctx, t, input))
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	failures := []scheduler.TargetOutcome{}
	for containerID, update := range containerUpdates {
		if update.Error != "" {
			failures = append(failures, scheduler.TargetOutcome{
				ResourceType: "container",
				ID:           containerID,
				Status:       scheduler.Failed,
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
	var request imageCheckInput
	if err := t.Payload(&request); err != nil {
		return nil, err
	}
	// Every check task has run, so the caller's credentials are no longer needed.
	s.requestCredentials.Delete(request.CredentialsKey)
	groups, results, err := imageCheckResults(t)
	if err != nil {
		return nil, err
	}
	var tagged tagCheckOutput
	if decodeErr := t.DecodeOutput("tags", &tagged); decodeErr != nil {
		return nil, decodeErr
	}
	checked, failed := 0, 0
	var targets []scheduler.TargetOutcome
	for index, group := range groups {
		checked += len(group)
		if results[index].Err == "" {
			continue
		}
		failed += len(group)
		for _, imageRef := range group {
			targets = append(targets, scheduler.TargetOutcome{
				ResourceType: "image",
				ID:           imageRef,
				Status:       scheduler.Failed,
				Message:      results[index].Err,
			})
		}
	}
	targets = append(targets, tagged.Failures...)
	// A failed flush is already logged; the records stay pending for the next one.
	_ = s.SendBatchUpdateNotifications(ctx)
	if request.All && (!request.Report || !t.Recovered()) {
		if cleanupErr := s.CleanupOrphanedRecords(ctx); cleanupErr != nil {
			slog.WarnContext(ctx, "failed to cleanup orphaned image update records after check-all", "error", cleanupErr.Error())
		}
	}
	outcome := scheduler.Outcome{
		Status: kit.Ternary(len(targets) > 0, scheduler.Partial, scheduler.Succeeded),
		Message: fmt.Sprintf("Image update check completed: %d checked, %d errors", checked, failed) +
			kit.Ternary(len(tagged.Failures) > 0, fmt.Sprintf(", %d tag policy errors", len(tagged.Failures)), ""),
		Targets: targets,
	}
	slog.InfoContext(ctx, "Image update check completed", "totalImages", checked, "failedImages", failed, "failedContainers", len(tagged.Failures))
	if !request.Report {
		return outcome, nil
	}
	reported := imageupdate.BatchResponse{}
	for index, group := range groups {
		if result := results[index].Value.Result; result != nil {
			for _, imageRef := range group {
				reported[imageRef] = new(*result)
			}
		}
	}
	return imageCheckOutput{
		Outcome:          outcome,
		Results:          reported,
		ContainerUpdates: tagged.Updates,
	}, nil
}

// imageCheckResults reads the discovered groups and their check slots.
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

// SendBatchUpdateNotifications delivers pending update notifications. It
// returns an error only when the pending set could not be determined, so a
// caller about to consume the records can stop before their notifications
// are lost; delivery failures leave the records unnotified for the next flush.
func (s *ImageUpdateService) SendBatchUpdateNotifications(ctx context.Context) error {
	// Serialize the query→send→mark sequence so the poll-end flush and the
	// updater consumption-path flush can't both read the same unnotified set
	// and double-send.
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()

	// completeImageUpdateActivity cancels the activity-tracked ctx before
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

	unnotifiedUpdates, err := func() (map[string]*ImageUpdateRecord, error) {
		records, recordsErr := s.GetUnnotifiedUpdates(notifCtx)
		if recordsErr != nil || len(records) == 0 {
			return records, recordsErr
		}
		// Records for resources that opted out of monitoring since the check, or
		// whose container is gone, stay unnotified: they resurface if monitoring
		// is re-enabled and are otherwise removed by orphan cleanup.
		dockerClient, clientErr := s.dockerClient(notifCtx)
		if clientErr != nil {
			return records, fmt.Errorf("resolve update-check eligibility: %w", clientErr)
		}
		apiCtx, apiCancel := s.dockerAPIContext(notifCtx)
		listed, listErr := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
		apiCancel()
		if listErr != nil {
			return records, fmt.Errorf("list containers for update-check eligibility: %w", listErr)
		}
		eligibility := newMonitoringEligibility(listed.Items)
		maps.DeleteFunc(records, func(id string, record *ImageUpdateRecord) bool {
			// Results scoped to a container that no longer exists are stale and not eligible.
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
		updatesToNotify := make(map[string]*imageupdate.Response)
		imageIDsToMark := make([]string, 0, len(unnotifiedUpdates))

		for imageID, record := range unnotifiedUpdates {
			updatesToNotify[record.Repository+":"+record.Tag+kit.Ternary(record.ContainerID != "", " (container "+record.ContainerID+")", "")] = &imageupdate.Response{
				HasUpdate:      record.HasUpdate,
				UpdateType:     record.UpdateType,
				CurrentVersion: record.CurrentVersion,
				LatestVersion:  kit.FromPtr(record.LatestVersion),
				CurrentDigest:  kit.FromPtr(record.CurrentDigest),
				LatestDigest:   kit.FromPtr(record.LatestDigest),
				CheckTime:      record.CheckTime,
				ResponseTimeMs: record.ResponseTimeMs,
				Error:          kit.FromPtr(record.LastError),
				AuthMethod:     kit.FromPtr(record.AuthMethod),
				AuthUsername:   kit.FromPtr(record.AuthUsername),
				AuthRegistry:   kit.FromPtr(record.AuthRegistry),
				UsedCredential: record.UsedCredential,
			}
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
	if s == nil || len(imageIDs) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Where("id IN ? OR image_id IN ?", imageIDs, imageIDs).Delete(&ImageUpdateRecord{}).Error; err != nil {
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
	if s == nil {
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

	return s.getUpdateSummaryForImageIDs(ctx, liveImageIDs)
}

func (s *ImageUpdateService) getUpdateSummaryForImageIDs(ctx context.Context, imageIDs []string) (*imageupdate.Summary, error) {
	if len(imageIDs) == 0 {
		return &imageupdate.Summary{}, nil
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

	return &imageupdate.Summary{
		TotalImages:       len(imageIDs),
		ImagesWithUpdates: int(aggregate.ImagesWithUpdates),
		DigestUpdates:     int(aggregate.DigestUpdates),
		ErrorsCount:       int(aggregate.ErrorsCount),
	}, nil
}

func (s *ImageUpdateService) checkContainerTagUpdates(ctx context.Context, imageRefs []string, credentials []containerregistry.Credential) (map[string]*imageupdate.Response, error) {
	wanted := map[string]bool{}
	for _, imageRef := range imageRefs {
		if normalized := refs.NormalizeImageUpdateRef(imageRef); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return map[string]*imageupdate.Response{}, nil
	}
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return map[string]*imageupdate.Response{}, err
	}
	apiCtx, cancel := s.dockerAPIContext(ctx)
	listed, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return map[string]*imageupdate.Response{}, fmt.Errorf("list containers for tag update checks: %w", err)
	}
	return s.tags.Check(ctx, listed.Items, wanted, credentials, func(deleteCtx context.Context, containerIDs []string) error {
		// Clear the stored results of containers whose policy no longer tracks tags, in bounded IN-list chunks.
		for chunk := range slices.Chunk(containerIDs, staleDigestRecordDeleteChunk) {
			if deleteErr := s.db.WithContext(deleteCtx).Where("container_id IN ?", chunk).Delete(&ImageUpdateRecord{}).Error; deleteErr != nil {
				return deleteErr
			}
		}
		return nil
	}, s.saveContainerTagResult)
}

func (s *ImageUpdateService) saveContainerTagResult(ctx context.Context, cnt container.Summary, result *imageupdate.Response) error {
	parsed, err := refs.NormalizeReference(cnt.Image)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	})
}
