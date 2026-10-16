package image

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/features"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/system"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	vulnerabilitytypes "github.com/getarcaneapp/arcane/types/v2/vulnerability"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image/children/patch"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
)

// projectIDCacheTTL bounds how long a snapshot of all (name → id) project rows is reused.
// Dashboard polls frequently; the projects table changes rarely, so a short TTL is safe.
const projectIDCacheTTL = 5 * time.Second

// patchSlot is one patch task's output; failures travel as task errors.
type patchSlot struct {
	Skipped bool `json:"skipped,omitempty"`
}

type ImageService struct {
	db                   *database.DB
	dockerService        *dockerInternal.DockerClientService
	imageUpdateService   *imageupdate.ImageUpdateService
	registryService      *registry.ContainerRegistryService
	vulnerabilityService *vulnerability.VulnerabilityService
	eventService         *event.EventService

	settingsService *settings.SettingsService
	patch           *patch.Service
	patchWorkflow   *flow.Workflow
	projectIDCache  *hot.HotCache[struct{}, map[string]string]
}

type imageRefUpdateLookup struct {
	originalRef          string
	tag                  string
	repositoryCandidates map[string]struct{}
}

type imageUpdateRecordKey struct {
	repository string
	tag        string
}

func NewImageService(
	db *database.DB,
	dockerService *dockerInternal.DockerClientService,
	registryService *registry.ContainerRegistryService,
	imageUpdateService *imageupdate.ImageUpdateService,
	vulnerabilityService *vulnerability.VulnerabilityService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	activityService *activity.ActivityService,
) *ImageService {
	return &ImageService{
		db:                   db,
		dockerService:        dockerService,
		registryService:      registryService,
		imageUpdateService:   imageUpdateService,
		vulnerabilityService: vulnerabilityService,
		eventService:         eventService,
		settingsService:      settingsService,
		patch:                patch.NewService(db, dockerService, settingsService, activityService, registryService, vulnerabilityService),
		projectIDCache: hot.NewHotCache[struct{}, map[string]string](hot.LRU, 1).
			WithTTL(projectIDCacheTTL).
			Build(),
	}
}

// RegisterWorkflows defines the scheduled auto-patch: find images with fixable
// findings, patch each one in turn, then report. Call it before the host starts.
func (s *ImageService) RegisterWorkflows(engine *flow.Engine) error {
	patchWorkflow, err := engine.Define(flow.Definition{
		Name:        "auto-patch",
		Version:     1,
		Fingerprint: "e399c9c07ea15a7d6a39f086529110040a4d2cc95daf72c1464363c7f5bb5efc",
		// Copacetic runs one BuildKit patch at a time and mirrors a process-wide logger.
		Concurrency: 1,
		Timeout:     12 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          activitytypes.TypeImagePatch,
			ResourceType:  new("images"),
			ResourceName:  new("Scheduled image patch"),
			Step:          "Finding patchable images",
			LatestMessage: "Scheduled image patch started",
		},
		Labels: map[string]string{
			"discover": "Finding patchable images",
			"patch":    "Patching images",
			"finalize": "Finishing image patch",
		},
		Steps: []workflow.StepSpec{
			workflow.Step("discover", engine.Handler(func(ctx context.Context, _ flow.Task) (any, error) {
				if !s.settingsService.IsFeatureEnabled(ctx, features.VulnerabilityManagement) {
					return []patch.Target{}, nil
				}
				targets, err := s.patch.Targets(ctx, types.LocalDockerEnvironmentID)
				if errors.Is(err, common.ErrPatchRequiresContainerdImageStore) {
					return nil, err
				}
				if err != nil {
					return nil, common.Classify(common.ErrUnavailable, err)
				}
				return targets, nil
			})),
			workflow.ForEach("patch", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
				var target patch.Target
				if err := t.DecodeItem(&target); err != nil {
					return nil, err
				}
				if !s.settingsService.IsFeatureEnabled(ctx, features.VulnerabilityManagement) {
					return patchSlot{Skipped: true}, nil
				}
				// The record ID is stable per task, so a redelivery resumes its own patch.
				return nil, s.patch.PatchTarget(ctx, types.LocalDockerEnvironmentID, target, fmt.Sprintf("%s-%d", t.ActivityID(), t.Index()))
			}),
				workflow.WithItemsFrom("discover"),
				workflow.WithMaxParallel(1),
				workflow.WithFailurePolicy(workflow.TolerateFailures),
				workflow.WithMaxAttempts(2),
				workflow.WithRetryBackoff(30*time.Second, 2*time.Minute)),
			workflow.Step("finalize", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
				var targets []patch.Target
				if err := t.DecodeOutput("discover", &targets); err != nil {
					return nil, err
				}
				slots, err := flow.Results[patchSlot](t, "patch")
				if err != nil {
					return nil, err
				}
				var failed []scheduler.TargetOutcome
				patched, skipped := 0, 0
				for index, slot := range slots {
					switch {
					case slot.Err != "":
						failed = append(failed, scheduler.TargetOutcome{
							ResourceType: "image",
							ID:           targets[index].ImageName,
							Status:       scheduler.Failed,
							Message:      slot.Err,
						})
					case slot.Value.Skipped:
						skipped++
					default:
						patched++
					}
				}
				status, message := func() (scheduler.RunStatus, string) {
					switch {
					case skipped > 0 || (len(targets) == 0 && !s.settingsService.IsFeatureEnabled(ctx, features.VulnerabilityManagement)):
						return kit.Ternary(patched > 0, scheduler.Partial, scheduler.Skipped),
							fmt.Sprintf("Remaining patches stopped because feature %s was disabled", features.VulnerabilityManagement)
					case len(failed) > 0:
						return scheduler.Partial, fmt.Sprintf("Image patch completed: %d patched, %d failed", patched, len(failed))
					default:
						return scheduler.Succeeded, fmt.Sprintf("Image patch completed: %d patched, %d failed", patched, len(failed))
					}
				}()
				return scheduler.Outcome{
					Status:  status,
					Message: message,
					Targets: failed,
				}, nil
			}), workflow.WithInputFrom("discover")),
		},
	})
	if err != nil {
		return err
	}
	s.patchWorkflow = patchWorkflow // keep: the workflow is defined after construction, once the flow engine exists
	return nil
}

// PatchWorkflow is the scheduled auto-patch workflow.
func (s *ImageService) PatchWorkflow() *flow.Workflow { return s.patchWorkflow }

// GetImageDetail returns a DetailSummary for the given image ID. It fetches ImageInspect
// and ImageList concurrently so the size field reflects the same metric shown in the
// image table (docker image ls / docker system df).
func (s *ImageService) GetImageDetail(ctx context.Context, id string) (*imagetypes.DetailSummary, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	var (
		inspect    image.InspectResponse
		listSize   int64
		containers []container.Summary
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "image worker")

		var inspectImageErr error
		inspectResult, inspectImageErr := dockerClient.ImageInspect(gctx, id)
		if inspectImageErr != nil {
			return fmt.Errorf("inspect not found: %w", inspectImageErr)
		}
		inspect = inspectResult.InspectResponse
		return nil
	})

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "image worker")

		images, imageListErr := s.dockerService.ListImages(gctx)
		if imageListErr != nil {
			return fmt.Errorf("failed to list images: %w", imageListErr)
		}
		for _, img := range images {
			if img.ID == id {
				listSize = img.Size
				break
			}
		}
		return nil
	})

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "image worker")

		containerList, listContainersErr := s.dockerService.ListContainers(gctx)
		if listContainersErr != nil {
			slog.DebugContext(gctx, "failed to list containers for image detail pinned references", "id", id, "error", listContainersErr)
			return nil
		}
		containers = containerList
		return nil
	})

	if waitErr := g.Wait(); waitErr != nil {
		return nil, waitErr
	}

	out := imagetypes.NewDetailSummary(&inspect)
	if listSize > 0 {
		out.Size = listSize            // keep: DetailSummary is mapped from the inspect response by imagetypes.NewDetailSummary
		out.Descriptor.Size = listSize // keep: DetailSummary is mapped from the inspect response by imagetypes.NewDetailSummary
	}
	if len(containers) > 0 {
		out.PinnedReferences = collectPinnedReferencesByImageID(containers)[inspect.ID] // keep: DetailSummary is mapped from the inspect response by imagetypes.NewDetailSummary
	}
	return &out, nil
}

func (s *ImageService) RemoveImage(ctx context.Context, id string, force bool, user usertypes.Actor) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", id, "", user.ID, user.Username, "0", err, database.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	imageDetails, inspectErr := dockerClient.ImageInspect(ctx, id)
	imageName := func() string {
		if inspectErr == nil && len(imageDetails.RepoTags) > 0 {
			return imageDetails.RepoTags[0]
		}
		return id
	}()

	defer s.eventService.BeginDockerResourceSuppressionWindow("image", id, imageName)()
	if inspectErr == nil {
		defer s.eventService.BeginDockerResourceSuppressionWindow("image", imageDetails.ID, "")()
		tagReleases := make([]func(), 0, len(imageDetails.RepoTags))
		defer func() {
			for _, release := range slices.Backward(tagReleases) {
				release()
			}
		}()
		for _, tag := range imageDetails.RepoTags {
			tagReleases = append(tagReleases, s.eventService.BeginDockerResourceSuppressionWindow("image", "", tag))
		}
	}
	removed, err := dockerClient.ImageRemove(ctx, id, client.ImageRemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", id, imageName, user.ID, user.Username, "0", err, database.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to remove image: %w", err)
	}

	s.cleanupImageUpdateRecords(ctx, append(getDeletedImageIDs(removed.Items), id))

	// Clean up vulnerability scan records for the deleted image
	if deleteScanResultErr := s.vulnerabilityService.DeleteScanResult(ctx, id); deleteScanResultErr != nil {
		slog.WarnContext(ctx, "failed to delete vulnerability scan record", "id", id, "error", deleteScanResultErr)
	}

	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageDelete, id, imageName, user.ID, user.Username, "0", database.JSON{
		"action":  "delete",
		"imageId": id,
		"force":   force,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log image deletion action", "err", logErr, "image", imageName, "imageId", id)
	}

	return nil
}

func (s *ImageService) PullImage(ctx context.Context, imageName string, progressWriter io.Writer, user usertypes.Actor, externalCreds []containerregistry.Credential) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", err, database.JSON{"action": "pull"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	slog.DebugContext(ctx, "Attempting to pull image", "image", imageName, "externalCredCount", len(externalCreds))

	pullOptions, err := s.PullOptionsWithAuth(ctx, imageName, externalCreds)
	if err != nil {
		slog.WarnContext(ctx, "Failed to get registry authentication for image; proceeding without auth", "image", imageName, "error", err.Error())
		pullOptions = client.ImagePullOptions{}
	}

	initialHasAuth := pullOptions.RegistryAuth != ""
	retriedWithoutAuth := false

	defer s.eventService.BeginDockerResourceSuppressionWindow("image", "", imageName)()
	reader, err := dockerClient.ImagePull(ctx, imageName, pullOptions)
	if err != nil && ShouldRetryAnonymousPull(pullOptions, err) {
		retriedWithoutAuth = true
		slog.WarnContext(ctx, "Docker ImagePull failed with registry auth; retrying anonymously", "image", imageName, "error", err.Error())
		pullOptions = client.ImagePullOptions{}
		reader, err = dockerClient.ImagePull(ctx, imageName, pullOptions)
	}
	if err != nil {
		slog.ErrorContext(
			ctx,
			"Docker ImagePull failed",
			"image",
			imageName,
			"hasAuth",
			pullOptions.RegistryAuth != "",
			"initialHasAuth",
			initialHasAuth,
			"retriedWithoutAuth",
			retriedWithoutAuth,
			"error",
			err.Error(),
		)
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", err, database.JSON{"action": "pull"})
		return fmt.Errorf("failed to initiate image pull for %s: %w", imageName, err)
	}
	defer func() { _ = reader.Close() }()

	logWriter := docker.NewLogLineWriter(progressWriter)
	streamErr := docker.RenderJSONMessageStream(reader, logWriter)
	_ = logWriter.Close()
	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) || strings.Contains(streamErr.Error(), "context canceled") {
			slog.DebugContext(ctx, "image pull stream canceled", "image", imageName, "err", streamErr)
			s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", streamErr, database.JSON{"action": "pull", "step": "canceled"})
			return fmt.Errorf("image pull stream canceled for %s: %w", imageName, streamErr)
		}
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", imageName, user.ID, user.Username, "0", streamErr, database.JSON{"action": "pull", "step": "read_stream"})
		return fmt.Errorf("error reading image pull stream for %s: %w", imageName, streamErr)
	}

	slog.DebugContext(ctx, "image pull stream completed", "image", imageName)

	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImagePull, "", imageName, user.ID, user.Username, "0", database.JSON{
		"action":    "pull",
		"imageName": imageName,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log image pull action", "err", logErr, "image", imageName)
	}
	if s.registryService != nil {
		if recordImagePullErr := s.registryService.RecordImagePull(ctx, imageName); recordImagePullErr != nil {
			slog.WarnContext(ctx, "failed to record registry pull count", "image", imageName, "error", recordImagePullErr)
		}
	}

	return nil
}

// ImageLastTagTime returns when the local image was last tagged, zero when the
// engine doesn't report it. Pull-policy refresh windows compare against this
// timestamp, the same clock compose v5.5.0 uses.
func (s *ImageService) ImageLastTagTime(ctx context.Context, imageName string) (time.Time, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	inspect, err := dockerClient.ImageInspect(ctx, imageName)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}
	return inspect.Metadata.LastTagTime, nil
}

func (s *ImageService) ReconcilePulledImageUpdate(ctx context.Context, imageName string) error {
	if s.imageUpdateService == nil {
		return nil
	}

	return s.imageUpdateService.MarkImageRefUpToDateAfterPull(ctx, imageName)
}

// TagImage adds a repository tag to an existing image.
func (s *ImageService) TagImage(ctx context.Context, source string, req imagetypes.TagRequest, user usertypes.Actor) error {
	source = strings.TrimSpace(source)
	repository := strings.TrimSpace(req.Repository)
	tag := strings.TrimSpace(req.Tag)
	if source == "" {
		return errors.New("image name is required")
	}
	if repository == "" {
		return errors.New("repository is required")
	}

	target := kit.Ternary(tag != "", repository+":"+tag, repository)

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", source, user.ID, user.Username, "0", err, database.JSON{"action": "tag", "target": target})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("image", "", target)()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: source, Target: target})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", source, user.ID, user.Username, "0", err, database.JSON{"action": "tag", "target": target})
		return fmt.Errorf("failed to tag image: %w", err)
	}

	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageTag, "", source, user.ID, user.Username, "0", database.JSON{
		"action":     "tag",
		"imageName":  source,
		"repository": repository,
		"tag":        tag,
		"target":     target,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log image tag action", "err", logErr, "image", source, "target", target)
	}

	return nil
}

// GetImageHistory returns layer history for an image.
func (s *ImageService) GetImageHistory(ctx context.Context, imageName string) ([]imagetypes.HistoryItem, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return nil, errors.New("image name is required")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	result, err := dockerClient.ImageHistory(ctx, imageName)
	if err != nil {
		return nil, fmt.Errorf("failed to get image history: %w", err)
	}

	items := make([]imagetypes.HistoryItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, imagetypes.HistoryItem{
			ID:        item.ID,
			Created:   item.Created,
			CreatedBy: item.CreatedBy,
			Tags:      item.Tags,
			Size:      item.Size,
			Comment:   item.Comment,
		})
	}
	return items, nil
}

// SearchImages searches the configured Docker registry for images.
func (s *ImageService) SearchImages(ctx context.Context, term string) ([]imagetypes.SearchResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, errors.New("search term is required")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	result, err := dockerClient.ImageSearch(ctx, term, client.ImageSearchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to search images: %w", err)
	}

	items := make([]imagetypes.SearchResult, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, imagetypes.SearchResult{
			Name:        item.Name,
			Description: item.Description,
			StarCount:   item.StarCount,
			Official:    item.IsOfficial,
		})
	}
	return items, nil
}

// ExportImage returns a tar stream for one Docker image.
func (s *ImageService) ExportImage(ctx context.Context, imageName string) (io.ReadCloser, error) {
	imageName = strings.TrimSpace(imageName)
	if imageName == "" {
		return nil, errors.New("image name is required")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	reader, err := dockerClient.ImageSave(ctx, []string{imageName})
	if err != nil {
		return nil, fmt.Errorf("failed to export image: %w", err)
	}
	return reader, nil
}

func (s *ImageService) LoadImageFromReader(ctx context.Context, reader io.Reader, fileName string, user usertypes.Actor, maxSizeBytes int64) (*imagetypes.LoadResult, error) {
	// Wrap reader with size limit enforcement
	limitedReader := io.LimitReader(reader, maxSizeBytes+1)

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", fileName, user.ID, user.Username, "0", err, database.JSON{"action": "load"})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// ImageLoad accepts a tar archive reader and optional load options
	loadResp, err := dockerClient.ImageLoad(ctx, limitedReader)
	if err != nil {
		// Check if error is due to size limit being exceeded
		if err.Error() == "unexpected EOF" || strings.Contains(err.Error(), "unexpected EOF") {
			return nil, fmt.Errorf("file size exceeds maximum allowed size of %d MB", maxSizeBytes/(1024*1024))
		}
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", fileName, user.ID, user.Username, "0", err, database.JSON{"action": "load", "file": fileName})
		return nil, fmt.Errorf("failed to load image from tar: %w", err)
	}
	defer func() { _ = loadResp.Close() }()

	var responseBuilder strings.Builder
	streamErr := docker.RenderJSONMessageStream(loadResp, &responseBuilder)
	if streamErr != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeImageError,
			"image",
			"",
			fileName,
			user.ID,
			user.Username,
			"0",
			streamErr,
			database.JSON{
				"action": "load",
				"file":   fileName,
				"step":   "read_response",
			},
		)
		return nil, fmt.Errorf("failed to read load response: %w", streamErr)
	}

	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageLoad, "", fileName, user.ID, user.Username, "0", database.JSON{
		"action":   "load",
		"fileName": fileName,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log image load action", "err", logErr, "file", fileName)
	}

	return &imagetypes.LoadResult{Stream: responseBuilder.String()}, nil
}

func (s *ImageService) ImageExistsLocally(ctx context.Context, imageName string) (bool, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.ImageInspect(ctx, imageName)
	if err == nil {
		return true, nil
	}

	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "no such image") || strings.Contains(errLower, "not found") {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}

func (s *ImageService) PullOptionsWithAuth(ctx context.Context, imageRef string, externalCreds []containerregistry.Credential) (client.ImagePullOptions, error) {
	registryHost := registryauth.ExtractRegistryHost(imageRef)

	// Check external credentials first
	for _, cred := range externalCreds {
		if !cred.Enabled || cred.Username == "" || cred.Token == "" {
			continue
		}

		if registryauth.IsRegistryMatch(cred.URL, registryHost) {
			authStr, err := registryauth.EncodeAuthHeader(cred.Username, cred.Token, registryauth.NormalizeRegistryURL(cred.URL))
			if err != nil {
				return client.ImagePullOptions{}, fmt.Errorf("failed to create auth header: %w", err)
			}

			slog.DebugContext(ctx, "Using external credentials for image pull", "registry", registryHost, "username", cred.Username)
			return client.ImagePullOptions{RegistryAuth: authStr}, nil
		}
	}

	authStr, err := s.registryService.GetRegistryAuthForHost(ctx, registryHost)
	if err != nil {
		return client.ImagePullOptions{}, fmt.Errorf("failed to get registry credentials: %w", err)
	}
	if authStr != "" {
		slog.DebugContext(ctx, "Using database credentials for image pull", "registry", registryHost)
	}

	return client.ImagePullOptions{RegistryAuth: authStr}, nil
}

func ShouldRetryAnonymousPull(pullOptions client.ImagePullOptions, pullErr error) bool {
	if pullOptions.RegistryAuth == "" || pullErr == nil {
		return false
	}
	return slices.ContainsFunc([]string{
		"unauthorized",
		"authentication required",
		"incorrect username or password",
		"no basic auth credentials",
		"access denied",
	}, func(indicator string) bool { return strings.Contains(strings.ToLower(pullErr.Error()), indicator) })
}

func (s *ImageService) PruneImages(ctx context.Context, options system.PruneImagesOptions) (*image.PruneReport, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs, err := func() (client.Filters, error) {
		switch options.Mode {
		case system.PruneImageModeNone:
			return nil, errors.New("image prune mode none is not allowed")
		case system.PruneImageModeDangling:
			return make(client.Filters).Add("dangling", "true"), nil
		case system.PruneImageModeAll:
			return make(client.Filters).Add("dangling", "false"), nil
		case system.PruneImageModeOlderThan:
			if strings.TrimSpace(options.Until) == "" {
				return nil, errors.New("image prune mode olderThan requires until")
			}
			return make(client.Filters).Add("until", options.Until), nil
		default:
			return nil, fmt.Errorf("unsupported image prune mode: %s", options.Mode)
		}
	}()
	if err != nil {
		return nil, err
	}

	report, err := dockerClient.ImagePrune(ctx, client.ImagePruneOptions{Filters: filterArgs})
	if err != nil {
		return nil, fmt.Errorf("failed to prune images: %w", err)
	}

	idsToDelete := getDeletedImageIDs(report.Report.ImagesDeleted)
	s.cleanupImageUpdateRecords(ctx, idsToDelete)
	if len(idsToDelete) > 0 {
		if deleteErr := s.vulnerabilityService.DeleteScanResultsByImageIDs(ctx, idsToDelete); deleteErr != nil {
			slog.WarnContext(ctx, "failed to delete vulnerability scan records after prune", "count", len(idsToDelete), "error", deleteErr)
		}
	}

	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageDelete, "", "bulk_prune", usertypes.SystemUser.ID, usertypes.SystemUser.Username, "0", database.JSON{
		"action":         "prune",
		"mode":           options.Mode,
		"until":          options.Until,
		"imagesDeleted":  len(report.Report.ImagesDeleted),
		"spaceReclaimed": report.Report.SpaceReclaimed,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log image prune action", "err", logErr)
	}

	return &report.Report, nil
}

func getDeletedImageIDs(deletedImages []image.DeleteResponse) []string {
	if len(deletedImages) == 0 {
		return nil
	}

	idsToDelete := make([]string, 0, len(deletedImages))
	for _, img := range deletedImages {
		if img.Deleted != "" {
			idsToDelete = append(idsToDelete, img.Deleted)
			continue
		}
		if img.Untagged != "" {
			idsToDelete = append(idsToDelete, img.Untagged)
		}
	}
	return idsToDelete
}

func (s *ImageService) cleanupImageUpdateRecords(ctx context.Context, idsToDelete []string) {
	if err := s.imageUpdateService.DeleteRecordsForImages(ctx, idsToDelete); err != nil {
		slog.WarnContext(ctx, "failed to clean up image update records", "error", err)
	}
	if err := s.imageUpdateService.CleanupOrphanedRecords(ctx); err != nil {
		slog.WarnContext(ctx, "failed to clean up orphaned image update records", "error", err)
	}
}

// GetUpdateInfoByImageIDs returns a map of image ID to UpdateInfo for the given image IDs.
// This is used by the container service to populate update info for containers.
func (s *ImageService) GetUpdateInfoByImageIDs(ctx context.Context, imageIDs []string) (map[string]*imagetypes.UpdateInfo, error) {
	if len(imageIDs) == 0 {
		return make(map[string]*imagetypes.UpdateInfo), nil
	}

	var updateRecords []imageupdate.ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("container_id = ? AND project_id = ? AND id IN ?", "", "", imageIDs).Find(&updateRecords).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch update records: %w", err)
	}

	result := make(map[string]*imagetypes.UpdateInfo, len(updateRecords))
	for i := range updateRecords {
		result[updateRecords[i].ID] = updateRecords[i].UpdateInfo()
	}

	return result, nil
}

// GetUpdateInfoByContainers returns the stored check matching each container's
// current policy, keyed by container ID. Digest policies read the image-level
// record for the container's runtime image ID, so a check recorded under another
// local tag of the same image still applies. Tag policies read the container's
// own record and require an exact policy key. Containers that opted out of update
// checks are omitted; disabling automatic installation does not hide results.
func (s *ImageService) GetUpdateInfoByContainers(ctx context.Context, containers []container.Summary) (map[string]*imagetypes.UpdateInfo, error) {
	result := make(map[string]*imagetypes.UpdateInfo)
	if len(containers) == 0 {
		return result, nil
	}
	digestImageIDs := make(map[string]string, len(containers))
	tagContainerIDs := make([]string, 0, len(containers))
	policies := make(map[string]string, len(containers))
	for _, cnt := range containers {
		if cnt.ID == "" || imageref.IsUpdateCheckDisabled(cnt.Labels) {
			continue
		}
		policy, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
		if policyErr == nil && policy.Strategy == "digest" {
			if imageID := strings.TrimSpace(cnt.ImageID); imageID != "" {
				digestImageIDs[cnt.ID] = imageID
			}
			continue
		}
		tagContainerIDs = append(tagContainerIDs, cnt.ID)
		policies[cnt.ID] = imageref.UpdatePolicyKey(cnt.Image, cnt.Labels)
	}

	if len(digestImageIDs) > 0 {
		imageIDs := make([]string, 0, len(digestImageIDs))
		seen := make(map[string]struct{}, len(digestImageIDs))
		for _, imageID := range digestImageIDs {
			if _, exists := seen[imageID]; exists {
				continue
			}
			seen[imageID] = struct{}{}
			imageIDs = append(imageIDs, imageID)
		}
		byImageID, err := s.GetUpdateInfoByImageIDs(ctx, imageIDs)
		if err != nil {
			return nil, err
		}
		for containerID, imageID := range digestImageIDs {
			if info := byImageID[imageID]; info != nil {
				// Each container gets its own copy so aggregation never mutates a shared record.
				result[containerID] = new(*info)
			}
		}
	}

	if len(tagContainerIDs) == 0 {
		return result, nil
	}
	var records []imageupdate.ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("container_id IN ?", tagContainerIDs).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch container update records: %w", err)
	}
	for i := range records {
		if records[i].PolicyKey == policies[records[i].ContainerID] {
			result[records[i].ContainerID] = records[i].UpdateInfo()
		}
	}
	return result, nil
}

// GetUpdateInfoByImageRefs returns persisted update information keyed by the
// original image reference string.
func (s *ImageService) GetUpdateInfoByImageRefs(ctx context.Context, imageRefs []string) (map[string]*imagetypes.UpdateInfo, error) {
	result := make(map[string]*imagetypes.UpdateInfo)
	if len(imageRefs) == 0 {
		return result, nil
	}

	lookups := make([]imageRefUpdateLookup, 0, len(imageRefs))
	seenRefs := make(map[string]struct{}, len(imageRefs))
	for _, rawRef := range imageRefs {
		originalRef, tag, repositoryCandidates, ok := imageref.ParseUpdateLookup(rawRef)
		if !ok {
			continue
		}
		if _, exists := seenRefs[originalRef]; exists {
			continue
		}
		seenRefs[originalRef] = struct{}{}
		lookups = append(lookups, imageRefUpdateLookup{
			originalRef:          originalRef,
			tag:                  tag,
			repositoryCandidates: repositoryCandidates,
		})
	}
	if len(lookups) == 0 {
		return result, nil
	}

	repositoryCandidates := make([]string, 0)
	repositorySeen := make(map[string]struct{})
	tags := make([]string, 0, len(lookups))
	tagSeen := make(map[string]struct{})

	for _, lookup := range lookups {
		if _, exists := tagSeen[lookup.tag]; !exists {
			tagSeen[lookup.tag] = struct{}{}
			tags = append(tags, lookup.tag)
		}
		for repo := range lookup.repositoryCandidates {
			if _, exists := repositorySeen[repo]; exists {
				continue
			}
			repositorySeen[repo] = struct{}{}
			repositoryCandidates = append(repositoryCandidates, repo)
		}
	}

	if len(repositoryCandidates) == 0 || len(tags) == 0 {
		return result, nil
	}

	var updateRecords []imageupdate.ImageUpdateRecord
	if err := s.db.WithContext(ctx).
		Where("container_id = ? AND project_id = ? AND tag IN ? AND repository IN ?", "", "", tags, repositoryCandidates).
		Order("check_time DESC").
		Find(&updateRecords).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch update records by image refs: %w", err)
	}

	// index maps each repository and tag to the position of its latest record. Ties keep the
	// earliest position, matching the order the records were fetched in.
	index := make(map[imageUpdateRecordKey]int, len(updateRecords))
	for i := range updateRecords {
		key := newImageUpdateRecordKey(updateRecords[i].Repository, updateRecords[i].Tag)
		if existing, ok := index[key]; !ok || updateRecords[i].CheckTime.After(updateRecords[existing].CheckTime) {
			index[key] = i
		}
	}
	for _, lookup := range lookups {
		// Pick the latest record across the lookup's repository aliases; equal check times fall back to fetch order.
		latest := -1
		for repository := range lookup.repositoryCandidates {
			i, ok := index[newImageUpdateRecordKey(repository, lookup.tag)]
			if !ok {
				continue
			}
			switch {
			case latest == -1,
				updateRecords[i].CheckTime.After(updateRecords[latest].CheckTime),
				updateRecords[i].CheckTime.Equal(updateRecords[latest].CheckTime) && i < latest:
				latest = i
			}
		}
		if latest != -1 {
			result[lookup.originalRef] = updateRecords[latest].UpdateInfo()
		}
	}

	return result, nil
}

func (s *ImageService) ListImagesPaginated(ctx context.Context, params pagination.QueryParams) ([]imagetypes.Summary, pagination.Response, error) {
	var (
		dockerImages  []image.Summary
		containers    []container.Summary
		updateRecords []imageupdate.ImageUpdateRecord
	)

	g, groupCtx := errgroup.WithContext(ctx)

	// Fetch Docker images
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "image worker")

		imageList, err := s.dockerService.ListImages(groupCtx)
		if err != nil {
			return fmt.Errorf("failed to list Docker images: %w", err)
		}
		dockerImages = imageList
		return nil
	})

	// Fetch containers to determine usage
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "image worker")

		containerList, err := s.dockerService.ListContainers(groupCtx)
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}
		containers = containerList
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, pagination.Response{}, err
	}

	imageIDs := make([]string, 0, len(dockerImages))
	for _, img := range dockerImages {
		imageIDs = append(imageIDs, img.ID)
	}

	if len(imageIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("id IN ? OR image_id IN ?", imageIDs, imageIDs).Find(&updateRecords).Error; err != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to fetch image update records: %w", err)
		}
	}

	vulnerabilityMap, err := func() (map[string]*vulnerabilitytypes.ScanSummary, error) {
		if len(imageIDs) == 0 {
			return nil, nil
		}
		return s.vulnerabilityService.GetScanSummariesByImageIDs(ctx, imageIDs)
	}()
	if err != nil {
		return nil, pagination.Response{}, err
	}

	result := s.getImagePaginationConfig().SearchOrderAndPaginate(MapDockerImagesToDTOs(
		dockerImages,
		containers,
		BuildVolumeUsageMap(containers, s.BuildProjectIDMap(ctx, containers)),
		buildUpdateMap(updateRecords),
		vulnerabilityMap,
	), params)

	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *ImageService) AggregateImages(ctx context.Context, params pagination.QueryParams) (imagetypes.ListResponse, error) {
	collection, err := httpx.CollectEnvironments[imagetypes.ListResponse](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return imagetypes.ListResponse{}, err
	}

	pages := make([]pagination.EnvironmentPage[imagetypes.Summary], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[imagetypes.Summary]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(s.getImagePaginationConfig(), pages, params)

	return imagetypes.ListResponse{
		Success:    true,
		Data:       data,
		Pagination: handlerutil.PaginationResponse(resp),
		MaxImageUploadSize: func() int {
			if len(collection.Results) == 0 {
				return 0
			}
			return collection.Results[0].Value.MaxImageUploadSize
		}(),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *ImageService) AggregateImageCounts(ctx context.Context) (base.ApiResponse[imagetypes.UsageCounts], error) {
	collection, err := httpx.CollectEnvironments[base.ApiResponse[imagetypes.UsageCounts]](ctx, nil)
	if err != nil {
		return base.ApiResponse[imagetypes.UsageCounts]{}, err
	}

	inuse, unused, total, totalSize := 0, 0, 0, int64(0)
	for _, result := range collection.Results {
		inuse += result.Value.Data.Inuse
		unused += result.Value.Data.Unused
		total += result.Value.Data.Total
		totalSize += result.Value.Data.TotalSize
	}

	return base.ApiResponse[imagetypes.UsageCounts]{
		Success: true,
		Data: imagetypes.UsageCounts{
			Inuse:     inuse,
			Unused:    unused,
			Total:     total,
			TotalSize: totalSize,
		},
		FailedEnvironments: collection.Failures,
	}, nil
}

func newImageUpdateRecordKey(repository, tag string) imageUpdateRecordKey {
	return imageUpdateRecordKey{repository: strings.TrimSpace(repository), tag: strings.ToLower(strings.TrimSpace(tag))}
}

// BuildProjectIDMap returns a map of compose project name → project ID for any
// containers that carry the com.docker.compose.project label. The lookup uses a
// short-TTL cache shared across all callers of this ImageService instance.
func (s *ImageService) BuildProjectIDMap(ctx context.Context, containers []container.Summary) map[string]string {
	projectIDs := make(map[string]string)
	projectNameSet := make(map[string]struct{})
	for _, c := range containers {
		if projectName := docker.ComposeProjectLabel(c.Labels); projectName != "" {
			projectNameSet[projectName] = struct{}{}
		}
	}
	if len(projectNameSet) == 0 {
		return projectIDs
	}

	stale, _ := s.projectIDCache.Peek(struct{}{})
	byName, _, err := s.projectIDCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]map[string]string, error) {
		var projects []struct {
			ID   string
			Name string
		}
		if err := s.db.WithContext(ctx).Table("projects").Select("id", "name").Find(&projects).Error; err != nil {
			return nil, err
		}

		loaded := make(map[string]string, len(projects))
		for _, project := range projects {
			loaded[project.Name] = project.ID
		}
		return map[struct{}]map[string]string{{}: loaded}, nil
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to load project ID map", "error", err)
	}
	all := kit.Ternary(err != nil, stale, byName)
	for name := range projectNameSet {
		if id, ok := all[name]; ok {
			projectIDs[name] = id
		}
	}
	return projectIDs
}

func BuildVolumeUsageMap(containers []container.Summary, projectIDByName map[string]string) map[string][]imagetypes.UsedBy {
	usageMap := make(map[string][]imagetypes.UsedBy)
	projectSeen := make(map[string]map[string]bool)
	containerSeen := make(map[string]map[string]bool)

	for _, c := range containers {
		if c.ImageID == "" {
			continue
		}

		projectName := docker.ComposeProjectLabel(c.Labels)

		if projectName != "" {
			if projectSeen[c.ImageID] == nil {
				projectSeen[c.ImageID] = make(map[string]bool)
			}
			if !projectSeen[c.ImageID][projectName] {
				usageMap[c.ImageID] = append(usageMap[c.ImageID], imagetypes.UsedBy{
					Type: "project",
					Name: projectName,
					ID:   projectIDByName[projectName],
				})
				projectSeen[c.ImageID][projectName] = true
			}
			continue
		}

		if containerSeen[c.ImageID] == nil {
			containerSeen[c.ImageID] = make(map[string]bool)
		}
		if !containerSeen[c.ImageID][c.ID] {
			usageMap[c.ImageID] = append(usageMap[c.ImageID], imagetypes.UsedBy{
				Type: "container",
				Name: cmp.Or(docker.ContainerNameFromNames(c.Names), c.ID),
				ID:   c.ID,
			})
			containerSeen[c.ImageID][c.ID] = true
		}
	}

	return usageMap
}

func buildUpdateMap(records []imageupdate.ImageUpdateRecord) map[string]*imageupdate.ImageUpdateRecord {
	updateMap := make(map[string]*imageupdate.ImageUpdateRecord, len(records))
	for i := range records {
		if records[i].ContainerID == "" {
			updateMap[records[i].ID] = &records[i]
		}
	}
	for i := range records {
		record := &records[i]
		if record.ContainerID == "" || record.ImageID == "" || !record.HasUpdate {
			continue
		}
		// A shared image can have several container policies and target tags.
		updateMap[record.ImageID] = &imageupdate.ImageUpdateRecord{
			ID:        record.ImageID,
			HasUpdate: true,
			CheckTime: func() time.Time {
				if existing := updateMap[record.ImageID]; existing != nil && existing.CheckTime.After(record.CheckTime) {
					return existing.CheckTime
				}
				return record.CheckTime
			}(),
		}
	}
	return updateMap
}

func collectPinnedReferencesByImageID(containers []container.Summary) map[string][]string {
	if len(containers) == 0 {
		return nil
	}

	seen := make(map[string]map[string]struct{})
	for _, c := range containers {
		if c.ImageID == "" || c.Image == "" {
			continue
		}
		trimmed := strings.TrimSpace(c.Image)
		if trimmed == "" || strings.HasPrefix(trimmed, "sha256:") {
			continue
		}

		named, err := reference.ParseNormalizedNamed(trimmed)
		if err != nil {
			continue
		}
		if _, ok := named.(reference.Digested); !ok {
			continue
		}

		if seen[c.ImageID] == nil {
			seen[c.ImageID] = make(map[string]struct{})
		}
		seen[c.ImageID][trimmed] = struct{}{}
	}

	if len(seen) == 0 {
		return nil
	}

	result := make(map[string][]string, len(seen))
	for imageID, refsMap := range seen {
		refs := slices.Sorted(maps.Keys(refsMap))
		result[imageID] = refs
	}

	return result
}

func MapDockerImagesToDTOs(
	dockerImages []image.Summary,
	containers []container.Summary,
	usageMap map[string][]imagetypes.UsedBy,
	updateMap map[string]*imageupdate.ImageUpdateRecord,
	vulnerabilityMap map[string]*vulnerabilitytypes.ScanSummary,
) []imagetypes.Summary {
	pinnedRefsByImageID := collectPinnedReferencesByImageID(containers)
	items := make([]imagetypes.Summary, 0, len(dockerImages))
	for _, di := range dockerImages {
		repo, tag := func() (string, string) {
			if len(di.RepoTags) > 0 {
				if named, err := reference.ParseNormalizedNamed(di.RepoTags[0]); err == nil {
					if tagged, ok := named.(reference.NamedTagged); ok {
						return reference.FamiliarName(named), tagged.Tag()
					}
					return reference.FamiliarName(named), "latest"
				}
				if before, after, found := strings.CutLast(di.RepoTags[0], ":"); found {
					return before, after
				}
				return di.RepoTags[0], "latest"
			}
			for _, rd := range di.RepoDigests {
				if candidateRepo, _, found := strings.CutLast(rd, "@"); rd != "<none>@<none>" && found && candidateRepo != "" {
					return candidateRepo, "<none>"
				}
			}
			return "<none>", "<none>"
		}()

		usedBy := usageMap[di.ID]
		items = append(items, imagetypes.Summary{
			ID:               di.ID,
			Repo:             repo,
			Tag:              tag,
			RepoTags:         di.RepoTags,
			RepoDigests:      di.RepoDigests,
			PinnedReferences: pinnedRefsByImageID[di.ID],
			Created:          di.Created,
			Size:             di.Size,
			VirtualSize:      di.SharedSize,
			Labels: func() map[string]any {
				if di.Labels == nil {
					return nil
				}
				labels, _ := kit.AsStringMap(di.Labels)
				return labels
			}(),
			InUse:  len(usedBy) > 0,
			UsedBy: usedBy,
			UpdateInfo: func() *imagetypes.UpdateInfo {
				if updateRecord, exists := updateMap[di.ID]; exists {
					return updateRecord.UpdateInfo()
				}
				return nil
			}(),
			VulnerabilityScan: vulnerabilityMap[di.ID],
		})
	}
	return items
}

func (s *ImageService) getImagePaginationConfig() pagination.Config[imagetypes.Summary] {
	return pagination.Config[imagetypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[imagetypes.Summary]{
			func(i imagetypes.Summary) (string, error) { return i.Repo, nil },
			func(i imagetypes.Summary) (string, error) { return i.Tag, nil },
			func(i imagetypes.Summary) (string, error) { return i.ID, nil },
			func(i imagetypes.Summary) (string, error) {
				if len(i.RepoTags) > 0 {
					return i.RepoTags[0], nil
				}
				return "", nil
			},
			func(i imagetypes.Summary) (string, error) {
				return strings.Join(i.PinnedReferences, " "), nil
			},
		},
		SortBindings: []pagination.SortBinding[imagetypes.Summary]{
			{
				Key: "repo",
				Fn: func(a, b imagetypes.Summary) int {
					if localCmp := strings.Compare(a.Repo, b.Repo); localCmp != 0 {
						return localCmp
					}
					if localCmp2 := strings.Compare(a.Tag, b.Tag); localCmp2 != 0 {
						return localCmp2
					}
					return strings.Compare(a.ID, b.ID)
				},
			},
			{
				Key: "tag",
				Fn: func(a, b imagetypes.Summary) int {
					return strings.Compare(a.Tag, b.Tag)
				},
			},
			{
				Key: "size",
				Fn: func(a, b imagetypes.Summary) int {
					if a.Size < b.Size {
						return -1
					}
					return kit.Ternary(a.Size > b.Size, 1, 0)
				},
			},
			{
				Key: "created",
				Fn: func(a, b imagetypes.Summary) int {
					if a.Created < b.Created {
						return -1
					}
					return kit.Ternary(a.Created > b.Created, 1, 0)
				},
			},
			{
				Key: "inUse",
				Fn: func(a, b imagetypes.Summary) int {
					if a.InUse == b.InUse {
						return 0
					}
					return kit.Ternary(a.InUse, -1, 1)
				},
			},
			{
				Key: "vulnerabilities",
				Fn: func(a, b imagetypes.Summary) int {
					counts := func(i imagetypes.Summary) vulnerabilitytypes.SeveritySummary {
						if kit.FromPtr(i.VulnerabilityScan).Status != vulnerabilitytypes.ScanStatusCompleted || kit.FromPtr(i.VulnerabilityScan).Summary == nil {
							return vulnerabilitytypes.SeveritySummary{Total: -1}
						}
						return *i.VulnerabilityScan.Summary
					}
					ac, bc := counts(a), counts(b)
					return cmp.Or(
						cmp.Compare(ac.Total, bc.Total),
						cmp.Compare(ac.Critical, bc.Critical),
						cmp.Compare(ac.High, bc.High),
					)
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[imagetypes.Summary]{
			{
				Key: "inUse",
				Fn: func(i imagetypes.Summary, filterValue string) bool {
					return kit.Ternary(filterValue == "true", i.InUse, filterValue != "false" || !i.InUse)
				},
			},
			{
				Key: "updates",
				Fn: func(i imagetypes.Summary, filterValue string) bool {
					switch filterValue {
					case "has_update":
						return kit.FromPtr(i.UpdateInfo).HasUpdate
					case "up_to_date":
						return i.UpdateInfo != nil && !i.UpdateInfo.HasUpdate && i.UpdateInfo.Error == ""
					case "error":
						return kit.FromPtr(i.UpdateInfo).Error != ""
					case "unknown":
						return i.UpdateInfo == nil
					default:
						value, valid := kit.ParseBool(filterValue)
						return !valid || kit.FromPtr(i.UpdateInfo).HasUpdate == value
					}
				},
			},
		},
	}
}
