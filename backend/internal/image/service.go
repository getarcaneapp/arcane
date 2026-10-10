package image

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
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
	"github.com/samber/mo"
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
	"github.com/getarcaneapp/arcane/backend/v2/internal/image/children/attestations"
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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
)

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
				outcome := scheduler.Outcome{Status: scheduler.Succeeded}
				patched, skipped := 0, 0
				for index, slot := range slots {
					switch {
					case slot.Err != "":
						outcome.Targets = append(outcome.Targets, scheduler.TargetOutcome{ResourceType: "image", ID: targets[index].ImageName, Status: scheduler.Failed, Message: slot.Err})
					case slot.Value.Skipped:
						skipped++
					default:
						patched++
					}
				}
				outcome.Message = fmt.Sprintf("Image patch completed: %d patched, %d failed", patched, len(outcome.Targets))
				switch {
				case skipped > 0 || (len(targets) == 0 && !s.settingsService.IsFeatureEnabled(ctx, features.VulnerabilityManagement)):
					outcome.Status = kit.Ternary(patched > 0, scheduler.Partial, scheduler.Skipped)
					outcome.Message = fmt.Sprintf("Remaining patches stopped because feature %s was disabled", features.VulnerabilityManagement)
				case len(outcome.Targets) > 0:
					outcome.Status = scheduler.Partial
				}
				return outcome, nil
			}), workflow.WithInputFrom("discover")),
		},
	})
	if err != nil {
		return err
	}
	s.patchWorkflow = patchWorkflow
	return nil
}

// PatchWorkflow is the scheduled auto-patch workflow.
func (s *ImageService) PatchWorkflow() *flow.Workflow { return s.patchWorkflow }

// newAttestationsServiceInternal builds the attestation reader from the image
// service's Docker client and registry credentials.
func (s *ImageService) newAttestationsServiceInternal() *attestations.Service {
	var dockerClient func(context.Context) (*client.Client, error)
	if s.dockerService != nil {
		dockerClient = s.dockerService.GetClient
	}
	var registryAuth func(context.Context, string) (string, error)
	if s.registryService != nil {
		registryAuth = s.registryService.GetRegistryAuthForHost
	}
	return attestations.NewService(dockerClient, registryAuth)
}

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

		imageList, imageListErr := dockerClient.ImageList(gctx, client.ImageListOptions{})
		if imageListErr != nil {
			return fmt.Errorf("failed to list images: %w", imageListErr)
		}
		for _, img := range imageList.Items {
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
		out.Size = listSize
		out.Descriptor.Size = listSize
	}
	if len(containers) > 0 {
		pinnedRefsMap := collectPinnedReferencesByImageIDInternal(containers)
		out.PinnedReferences = pinnedRefsMap[inspect.ID]
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
	var imageName string
	if inspectErr == nil && len(imageDetails.RepoTags) > 0 {
		imageName = imageDetails.RepoTags[0]
	} else {
		imageName = id
	}

	options := client.ImageRemoveOptions{
		Force:         force,
		PruneChildren: true,
	}

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
	removed, err := dockerClient.ImageRemove(ctx, id, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", id, imageName, user.ID, user.Username, "0", err, database.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to remove image: %w", err)
	}

	idsToDelete := append(getDeletedImageIDsInternal(removed.Items), id)
	s.cleanupImageUpdateRecordsInternal(ctx, idsToDelete)

	// Clean up vulnerability scan records for the deleted image
	if s.vulnerabilityService != nil {
		if deleteScanResultErr := s.vulnerabilityService.DeleteScanResult(ctx, id); deleteScanResultErr != nil {
			slog.WarnContext(ctx, "failed to delete vulnerability scan record", "id", id, "error", deleteScanResultErr)
		}
	}

	metadata := database.JSON{
		"action":  "delete",
		"imageId": id,
		"force":   force,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageDelete, id, imageName, user.ID, user.Username, "0", metadata); logErr != nil {
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

	metadata := database.JSON{
		"action":    "pull",
		"imageName": imageName,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImagePull, "", imageName, user.ID, user.Username, "0", metadata); logErr != nil {
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

	metadata := database.JSON{
		"action":     "tag",
		"imageName":  source,
		"repository": repository,
		"tag":        tag,
		"target":     target,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageTag, "", source, user.ID, user.Username, "0", metadata); logErr != nil {
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

	var result imagetypes.LoadResult
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

	result.Stream = responseBuilder.String()

	metadata := database.JSON{
		"action":   "load",
		"fileName": fileName,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageLoad, "", fileName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log image load action", "err", logErr, "file", fileName)
	}

	return &result, nil
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
	pullOptions := client.ImagePullOptions{}

	registryHost := registryauth.ExtractRegistryHost(imageRef)

	// Check external credentials first
	for _, cred := range externalCreds {
		if !cred.Enabled || cred.Username == "" || cred.Token == "" {
			continue
		}

		if registryauth.IsRegistryMatch(cred.URL, registryHost) {
			authStr, err := registryauth.EncodeAuthHeader(cred.Username, cred.Token, registryauth.NormalizeRegistryURL(cred.URL))
			if err != nil {
				return pullOptions, fmt.Errorf("failed to create auth header: %w", err)
			}
			pullOptions.RegistryAuth = authStr

			slog.DebugContext(ctx, "Using external credentials for image pull", "registry", registryHost, "username", cred.Username)
			return pullOptions, nil
		}
	}

	if s.registryService == nil {
		return pullOptions, nil
	}

	authStr, err := s.registryService.GetRegistryAuthForHost(ctx, registryHost)
	if err != nil {
		return pullOptions, fmt.Errorf("failed to get registry credentials: %w", err)
	}
	if authStr != "" {
		pullOptions.RegistryAuth = authStr
		slog.DebugContext(ctx, "Using database credentials for image pull", "registry", registryHost)
	}

	return pullOptions, nil
}

func ShouldRetryAnonymousPull(pullOptions client.ImagePullOptions, pullErr error) bool {
	if pullOptions.RegistryAuth == "" || pullErr == nil {
		return false
	}
	return isUnauthorizedPullErrorInternal(pullErr)
}

func isUnauthorizedPullErrorInternal(err error) bool {
	if err == nil {
		return false
	}

	errLower := strings.ToLower(err.Error())
	unauthorizedIndicators := []string{
		"unauthorized",
		"authentication required",
		"incorrect username or password",
		"no basic auth credentials",
		"access denied",
	}

	for _, indicator := range unauthorizedIndicators {
		if strings.Contains(errLower, indicator) {
			return true
		}
	}
	return false
}

func (s *ImageService) PruneImages(ctx context.Context, options system.PruneImagesOptions) (*image.PruneReport, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	switch options.Mode {
	case system.PruneImageModeNone:
		return nil, errors.New("image prune mode none is not allowed")
	case system.PruneImageModeDangling:
		filterArgs = filterArgs.Add("dangling", "true")
	case system.PruneImageModeAll:
		filterArgs = filterArgs.Add("dangling", "false")
	case system.PruneImageModeOlderThan:
		if strings.TrimSpace(options.Until) == "" {
			return nil, errors.New("image prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	default:
		return nil, fmt.Errorf("unsupported image prune mode: %s", options.Mode)
	}

	report, err := dockerClient.ImagePrune(ctx, client.ImagePruneOptions{Filters: filterArgs})
	if err != nil {
		return nil, fmt.Errorf("failed to prune images: %w", err)
	}
	pruneReport := report.Report

	idsToDelete := getDeletedImageIDsInternal(pruneReport.ImagesDeleted)
	s.cleanupImageUpdateRecordsInternal(ctx, idsToDelete)
	s.cleanupVulnerabilityRecordsAfterPruneInternal(ctx, idsToDelete)

	metadata := database.JSON{
		"action":         "prune",
		"mode":           options.Mode,
		"until":          options.Until,
		"imagesDeleted":  len(pruneReport.ImagesDeleted),
		"spaceReclaimed": pruneReport.SpaceReclaimed,
	}
	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageDelete, "", "bulk_prune", usertypes.SystemUser.ID, usertypes.SystemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log image prune action", "err", logErr)
	}

	return &pruneReport, nil
}

func getDeletedImageIDsInternal(deletedImages []image.DeleteResponse) []string {
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

func (s *ImageService) cleanupVulnerabilityRecordsAfterPruneInternal(ctx context.Context, idsToDelete []string) {
	if s.vulnerabilityService == nil || len(idsToDelete) == 0 {
		return
	}

	if err := s.vulnerabilityService.DeleteScanResultsByImageIDs(ctx, idsToDelete); err != nil {
		slog.WarnContext(ctx, "failed to delete vulnerability scan records after prune", "count", len(idsToDelete), "error", err)
	}
}

func (s *ImageService) cleanupImageUpdateRecordsInternal(ctx context.Context, idsToDelete []string) {
	if s.imageUpdateService == nil {
		return
	}

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
	if s.db == nil || len(imageIDs) == 0 {
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
	if s.db == nil || len(containers) == 0 {
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
				scoped := *info
				result[containerID] = &scoped
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

type imageRefUpdateLookup struct {
	originalRef          string
	tag                  string
	repositoryCandidates map[string]struct{}
}

// GetUpdateInfoByImageRefs returns persisted update information keyed by the
// original image reference string.
func (s *ImageService) GetUpdateInfoByImageRefs(ctx context.Context, imageRefs []string) (map[string]*imagetypes.UpdateInfo, error) {
	result := make(map[string]*imagetypes.UpdateInfo)
	if s.db == nil || len(imageRefs) == 0 {
		return result, nil
	}

	lookups := buildImageRefUpdateLookupsInternal(imageRefs)
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

	index := indexLatestImageUpdateRecordsInternal(updateRecords)
	for _, lookup := range lookups {
		if record := selectLatestMatchingImageUpdateRecordInternal(lookup, updateRecords, index); record != nil {
			result[lookup.originalRef] = record.UpdateInfo()
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

		var err error
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

		var err error
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

	if s.db != nil && len(imageIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("id IN ? OR image_id IN ?", imageIDs, imageIDs).Find(&updateRecords).Error; err != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to fetch image update records: %w", err)
		}
	}

	var vulnerabilityMap map[string]*vulnerabilitytypes.ScanSummary
	if s.vulnerabilityService != nil && len(imageIDs) > 0 {
		var err error
		vulnerabilityMap, err = s.vulnerabilityService.GetScanSummariesByImageIDs(ctx, imageIDs)
		if err != nil {
			return nil, pagination.Response{}, err
		}
	}

	projectIDByName := s.BuildProjectIDMap(ctx, containers)
	usageMap := BuildVolumeUsageMap(containers, projectIDByName)
	updateMap := buildUpdateMapInternal(updateRecords)

	items := MapDockerImagesToDTOs(dockerImages, containers, usageMap, updateMap, vulnerabilityMap)

	config := s.getImagePaginationConfig()

	result := config.SearchOrderAndPaginate(items, params)

	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func buildImageRefUpdateLookupsInternal(imageRefs []string) []imageRefUpdateLookup {
	lookups := make([]imageRefUpdateLookup, 0, len(imageRefs))
	seen := make(map[string]struct{}, len(imageRefs))

	for _, rawRef := range imageRefs {
		originalRef, tag, repositoryCandidates, ok := imageref.ParseUpdateLookup(rawRef)
		if !ok {
			continue
		}
		lookup := imageRefUpdateLookup{originalRef: originalRef, tag: tag, repositoryCandidates: repositoryCandidates}
		if _, exists := seen[lookup.originalRef]; exists {
			continue
		}
		seen[lookup.originalRef] = struct{}{}
		lookups = append(lookups, lookup)
	}

	return lookups
}

type imageUpdateRecordKeyInternal struct {
	repository string
	tag        string
}

func newImageUpdateRecordKeyInternal(repository, tag string) imageUpdateRecordKeyInternal {
	return imageUpdateRecordKeyInternal{repository: strings.TrimSpace(repository), tag: strings.ToLower(strings.TrimSpace(tag))}
}

// indexLatestImageUpdateRecordsInternal maps each repository and tag to the
// position of its latest record. Ties keep the earliest position, matching
// the order the records were fetched in.
func indexLatestImageUpdateRecordsInternal(updateRecords []imageupdate.ImageUpdateRecord) map[imageUpdateRecordKeyInternal]int {
	index := make(map[imageUpdateRecordKeyInternal]int, len(updateRecords))
	for i := range updateRecords {
		key := newImageUpdateRecordKeyInternal(updateRecords[i].Repository, updateRecords[i].Tag)
		if existing, ok := index[key]; !ok || updateRecords[i].CheckTime.After(updateRecords[existing].CheckTime) {
			index[key] = i
		}
	}
	return index
}

// selectLatestMatchingImageUpdateRecordInternal picks the latest record across
// the lookup's repository aliases; equal check times fall back to fetch order.
func selectLatestMatchingImageUpdateRecordInternal(
	lookup imageRefUpdateLookup,
	updateRecords []imageupdate.ImageUpdateRecord,
	index map[imageUpdateRecordKeyInternal]int,
) *imageupdate.ImageUpdateRecord {
	latest := -1
	for repository := range lookup.repositoryCandidates {
		i, ok := index[newImageUpdateRecordKeyInternal(repository, lookup.tag)]
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
	if latest == -1 {
		return nil
	}
	return &updateRecords[latest]
}

func convertLabels(labels map[string]string) map[string]any {
	if labels == nil {
		return nil
	}
	result, _ := kit.AsStringMap(labels)
	return result
}

// projectIDCacheTTL bounds how long a snapshot of all (name → id) project rows is reused.
// Dashboard polls frequently; the projects table changes rarely, so a short TTL is safe.
const projectIDCacheTTL = 5 * time.Second

func (s *ImageService) loadProjectIDByNameCachedInternal(ctx context.Context) map[string]string {
	if s.projectIDCache == nil {
		s.projectIDCache = hot.NewHotCache[struct{}, map[string]string](hot.LRU, 1).
			WithTTL(projectIDCacheTTL).
			Build()
	}
	stale, staleFound := s.projectIDCache.Peek(struct{}{})
	byName, found, err := s.projectIDCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]map[string]string, error) {
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
		return kit.Ternary(staleFound, stale, map[string]string{})
	}
	return kit.Ternary(!found, map[string]string{}, byName)
}

// BuildProjectIDMap returns a map of compose project name → project ID for any
// containers that carry the com.docker.compose.project label. The lookup uses a
// short-TTL cache shared across all callers of this ImageService instance.
func (s *ImageService) BuildProjectIDMap(ctx context.Context, containers []container.Summary) map[string]string {
	projectIDs := make(map[string]string)
	if s.db == nil {
		return projectIDs
	}

	projectNameSet := make(map[string]struct{})
	for _, c := range containers {
		if c.Labels == nil {
			continue
		}
		if projectName := docker.ComposeProjectLabel(c.Labels); projectName != "" {
			projectNameSet[projectName] = struct{}{}
		}
	}
	if len(projectNameSet) == 0 {
		return projectIDs
	}

	all := s.loadProjectIDByNameCachedInternal(ctx)
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
			projectID := projectIDByName[projectName]
			if projectSeen[c.ImageID] == nil {
				projectSeen[c.ImageID] = make(map[string]bool)
			}
			if !projectSeen[c.ImageID][projectName] {
				usedBy := imagetypes.UsedBy{
					Type: "project",
					Name: projectName,
				}
				usedBy.ID = cmp.Or(projectID, usedBy.ID)
				usageMap[c.ImageID] = append(usageMap[c.ImageID], usedBy)
				projectSeen[c.ImageID][projectName] = true
			}
			continue
		}

		containerName := cmp.Or(docker.ContainerNameFromNames(c.Names), c.ID)

		if containerSeen[c.ImageID] == nil {
			containerSeen[c.ImageID] = make(map[string]bool)
		}
		if !containerSeen[c.ImageID][c.ID] {
			usageMap[c.ImageID] = append(usageMap[c.ImageID], imagetypes.UsedBy{
				Type: "container",
				Name: containerName,
				ID:   c.ID,
			})
			containerSeen[c.ImageID][c.ID] = true
		}
	}

	return usageMap
}

func buildUpdateMapInternal(records []imageupdate.ImageUpdateRecord) map[string]*imageupdate.ImageUpdateRecord {
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
		aggregate := &imageupdate.ImageUpdateRecord{ID: record.ImageID, HasUpdate: true, CheckTime: record.CheckTime}
		if existing := updateMap[record.ImageID]; existing != nil && existing.CheckTime.After(aggregate.CheckTime) {
			aggregate.CheckTime = existing.CheckTime
		}
		updateMap[record.ImageID] = aggregate
	}
	return updateMap
}

func parseRepoAndTagFromRepoTag(repoTag string) (repo, tag string) {
	if named, err := reference.ParseNormalizedNamed(repoTag); err == nil {
		repo = reference.FamiliarName(named)
		if tagged, ok := named.(reference.NamedTagged); ok {
			tag = tagged.Tag()
		} else {
			tag = "latest"
		}
		return repo, tag
	}

	if before, after, found := strings.CutLast(repoTag, ":"); found {
		return before, after
	}
	return repoTag, "latest"
}

func parseRepoFromDigests(repoDigests []string) mo.Option[string] {
	for _, rd := range repoDigests {
		if rd == "<none>@<none>" {
			continue
		}
		if candidateRepo, _, found := strings.CutLast(rd, "@"); found && candidateRepo != "" {
			return mo.Some(candidateRepo)
		}
	}
	return mo.None[string]()
}

func determineRepoAndTag(di image.Summary) (repo, tag string) {
	if len(di.RepoTags) > 0 {
		return parseRepoAndTagFromRepoTag(di.RepoTags[0])
	}

	if len(di.RepoDigests) > 0 {
		if localRepo, found := parseRepoFromDigests(di.RepoDigests).Get(); found {
			return localRepo, "<none>"
		}
	}

	return "<none>", "<none>"
}

func collectPinnedReferencesByImageIDInternal(containers []container.Summary) map[string][]string {
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
	pinnedRefsByImageID := collectPinnedReferencesByImageIDInternal(containers)
	items := make([]imagetypes.Summary, 0, len(dockerImages))
	for _, di := range dockerImages {
		repo, tag := determineRepoAndTag(di)

		usedBy := usageMap[di.ID]
		imageDto := imagetypes.Summary{
			ID:               di.ID,
			Repo:             repo,
			Tag:              tag,
			RepoTags:         di.RepoTags,
			RepoDigests:      di.RepoDigests,
			PinnedReferences: pinnedRefsByImageID[di.ID],
			Created:          di.Created,
			Size:             di.Size,
			VirtualSize:      di.SharedSize,
			Labels:           convertLabels(di.Labels),
			InUse:            len(usedBy) > 0,
			UsedBy:           usedBy,
		}

		if updateRecord, exists := updateMap[di.ID]; exists {
			imageDto.UpdateInfo = updateRecord.UpdateInfo()
		}

		if vulnerabilityMap != nil {
			if summary, exists := vulnerabilityMap[di.ID]; exists {
				imageDto.VulnerabilityScan = summary
			}
		}

		items = append(items, imageDto)
	}
	return items
}

func vulnerabilitySortCountsInternal(i imagetypes.Summary) vulnerabilitytypes.SeveritySummary {
	scan := i.VulnerabilityScan
	if scan == nil || scan.Status != vulnerabilitytypes.ScanStatusCompleted || scan.Summary == nil {
		return vulnerabilitytypes.SeveritySummary{Total: -1}
	}
	return *scan.Summary
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
					ac, bc := vulnerabilitySortCountsInternal(a), vulnerabilitySortCountsInternal(b)
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
						return i.UpdateInfo != nil && i.UpdateInfo.HasUpdate
					case "up_to_date":
						return i.UpdateInfo != nil && !i.UpdateInfo.HasUpdate && i.UpdateInfo.Error == ""
					case "error":
						return i.UpdateInfo != nil && i.UpdateInfo.Error != ""
					case "unknown":
						return i.UpdateInfo == nil
					default:
						value, valid := kit.ParseBool(filterValue)
						hasUpdate := i.UpdateInfo != nil && i.UpdateInfo.HasUpdate
						return !valid || hasUpdate == value
					}
				},
			},
		},
	}
}
