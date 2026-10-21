package prune

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// Service runs manual and scheduled prunes, serializing them per environment.
type Service struct {
	location           *time.Location
	dockerService      *dockerInternal.DockerClientService
	activityService    *activity.ActivityService
	imageUpdateService *imageupdate.ImageUpdateService
	volumeService      *volume.VolumeService
	networkService     *network.NetworkService
	pruneMu            sync.Mutex
	runningPrunes      map[string]string
}

func NewService(
	location *time.Location,
	dockerService *dockerInternal.DockerClientService,
	activityService *activity.ActivityService,
	imageUpdateService *imageupdate.ImageUpdateService,
	volumeService *volume.VolumeService,
	networkService *network.NetworkService,
) *Service {
	return &Service{
		location:           location,
		dockerService:      dockerService,
		activityService:    activityService,
		imageUpdateService: imageUpdateService,
		volumeService:      volumeService,
		networkService:     networkService,
		runningPrunes:      make(map[string]string),
	}
}

func (s *Service) PruneAll(ctx context.Context, environmentID string, req types.PruneAllRequest) (*types.PruneAllResult, bool, error) {
	slog.InfoContext(ctx, "Starting selective prune operation",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"buildCache", req.BuildCache,
	)

	prune := s.beginSystemPrune(ctx, environmentID, req)
	if prune.started.IsAbsent() {
		slog.InfoContext(ctx, "System prune already running", "environmentId", environmentID, "activityId", prune.activityID)
		return &types.PruneAllResult{
			Success:    true,
			ActivityID: mo.EmptyableToOption(strings.TrimSpace(prune.activityID)).ToPointer(),
		}, false, nil
	}

	defer s.finishSystemPrune(environmentID)

	ctx = s.activityService.Track(ctx, prune.activityID)
	if err := jobcontext.Progress(ctx, types.TargetOutcome{
		ID:         "scheduled-prune",
		Status:     types.RunStatusRunning,
		ActivityID: prune.activityID,
	}); err != nil {
		result := &types.PruneAllResult{
			Success:    false,
			Errors:     []string{err.Error()},
			ActivityID: mo.EmptyableToOption(strings.TrimSpace(prune.activityID)).ToPointer(),
		}
		s.completeSystemPruneActivity(ctx, prune.activityID, result)
		return result, true, err
	}
	return s.runSystemPrune(ctx, req, prune.activityID), true, nil
}

func (s *Service) StartPruneAll(ctx context.Context, environmentID string, req types.PruneAllRequest) *types.PruneAllResult {
	prune := s.beginSystemPrune(ctx, environmentID, req)
	if prune.started.IsAbsent() {
		slog.InfoContext(ctx, "System prune already running", "environmentId", environmentID, "activityId", prune.activityID)
		return &types.PruneAllResult{
			Success:    true,
			ActivityID: mo.EmptyableToOption(strings.TrimSpace(prune.activityID)).ToPointer(),
		}
	}

	backgroundCtx := s.activityService.Track(utils.ActivityRuntimeContext(ctx, nil), prune.activityID)
	go func() {
		defer s.finishSystemPrune(environmentID)
		s.runSystemPrune(backgroundCtx, req, prune.activityID)
	}()

	return &types.PruneAllResult{
		Success:    true,
		ActivityID: mo.EmptyableToOption(strings.TrimSpace(prune.activityID)).ToPointer(),
	}
}

type systemPruneBeginResult struct {
	activityID string
	started    mo.Option[struct{}]
}

func (s *Service) beginSystemPrune(ctx context.Context, environmentID string, req types.PruneAllRequest) systemPruneBeginResult {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	if activityID, ok := s.runningPrunes[environmentID]; ok {
		return systemPruneBeginResult{activityID: activityID, started: mo.None[struct{}]()}
	}

	localActivity, err := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
		EnvironmentID: environmentID,
		Type:          types.ActivityTypeSystemPrune,
		ResourceType:  new("system"),
		ResourceName:  new("Docker resources"),
		Step:          "Preparing prune",
		LatestMessage: "System prune started",
		Metadata: database.JSON{
			"containers": req.Containers,
			"images":     req.Images,
			"volumes":    req.Volumes,
			"networks":   req.Networks,
			"buildCache": req.BuildCache,
		},
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start system prune activity", "error", err)
	}
	activityID := kit.Ternary(err != nil, "", kit.FromPtr(localActivity).ID)
	s.runningPrunes[environmentID] = activityID
	return systemPruneBeginResult{activityID: activityID, started: mo.Some(struct{}{})}
}

func (s *Service) finishSystemPrune(environmentID string) {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	delete(s.runningPrunes, environmentID)
}

func (s *Service) runSystemPrune(ctx context.Context, req types.PruneAllRequest, activityID string) *types.PruneAllResult {
	var mu sync.Mutex
	var errs []string
	addError := func(format string, err error) {
		mu.Lock()
		defer mu.Unlock()
		errs = append(errs, fmt.Sprintf(format, err))
	}
	var containers, images, buildCache, volumes, networks types.PruneAllResult

	// 1. Prune Containers first (sequential) as it may free up other resources
	if req.Containers != nil && req.Containers.Mode != types.PruneContainerModeNone {
		s.appendSystemPruneActivityMessage(ctx, activityID, "Pruning containers", 15)
		slog.InfoContext(ctx, "Pruning containers...", "mode", req.Containers.Mode, "until", req.Containers.Until)
		part, err := s.pruneContainers(ctx, *req.Containers)
		if err != nil {
			addError("Container pruning failed: %v", err)
		}
		containers = part
	}

	// 2. Prune other resources in parallel
	g, groupCtx := errgroup.WithContext(ctx)

	if req.Images != nil && req.Images.Mode != types.PruneImageModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessage(groupCtx, activityID, "Pruning images", 40)
			slog.InfoContext(groupCtx, "Pruning images...", "mode", req.Images.Mode, "until", req.Images.Until)
			part, err := s.pruneImages(groupCtx, *req.Images)
			if err != nil {
				addError("Image pruning failed: %v", err)
			}
			images = part
			return nil
		})
	}

	if req.BuildCache != nil && req.BuildCache.Mode != types.PruneBuildCacheModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessage(groupCtx, activityID, "Pruning build cache", 45)
			slog.InfoContext(groupCtx, "Pruning build cache...", "mode", req.BuildCache.Mode, "until", req.BuildCache.Until)
			part, err := s.pruneBuildCache(groupCtx, *req.BuildCache)
			if err != nil {
				slog.WarnContext(groupCtx, "Build cache pruning encountered an error", "error", err.Error())
				// Surface the failure like every other prune type so a build cache that
				// could not be reclaimed is reported instead of silently left behind.
				addError("Build cache pruning failed: %v", err)
			}
			buildCache = part
			return nil
		})
	}

	if req.Volumes != nil && req.Volumes.Mode != types.PruneVolumeModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessage(groupCtx, activityID, "Pruning volumes", 55)
			slog.InfoContext(groupCtx, "Pruning volumes...", "mode", req.Volumes.Mode)
			part, err := s.pruneVolumes(groupCtx, *req.Volumes)
			if err != nil {
				addError("Volume pruning failed: %v", err)
			}
			volumes = part
			return nil
		})
	}

	if req.Networks != nil && req.Networks.Mode != types.PruneNetworkModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessage(groupCtx, activityID, "Pruning networks", 65)
			slog.InfoContext(groupCtx, "Pruning networks...", "mode", req.Networks.Mode, "until", req.Networks.Until)
			part, err := s.pruneNetworks(groupCtx, *req.Networks)
			if err != nil {
				addError("Network pruning failed: %v", err)
			}
			networks = part
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "Prune operations failed", "error", err)
		addError("Prune worker failed: %v", err)
	}

	result := &types.PruneAllResult{
		ContainersPruned:         containers.ContainersPruned,
		ImagesDeleted:            images.ImagesDeleted,
		VolumesDeleted:           volumes.VolumesDeleted,
		NetworksDeleted:          networks.NetworksDeleted,
		SpaceReclaimed:           containers.SpaceReclaimed + images.SpaceReclaimed + buildCache.SpaceReclaimed + volumes.SpaceReclaimed,
		ContainerSpaceReclaimed:  containers.ContainerSpaceReclaimed,
		ImageSpaceReclaimed:      images.ImageSpaceReclaimed,
		VolumeSpaceReclaimed:     volumes.VolumeSpaceReclaimed,
		BuildCacheSpaceReclaimed: buildCache.BuildCacheSpaceReclaimed,
		Success:                  len(errs) == 0,
		Errors:                   errs,
		ActivityID:               mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
	}
	slog.InfoContext(
		ctx,
		"Selective prune operation completed",
		"success",
		result.Success,
		"containersPruned",
		len(
			result.ContainersPruned,
		),
		"imagesDeleted",
		len(
			result.ImagesDeleted,
		),
		"volumesDeleted",
		len(
			result.VolumesDeleted,
		),
		"networksDeleted",
		len(
			result.NetworksDeleted,
		),
		"spaceReclaimed",
		result.SpaceReclaimed,
		"errorCount",
		len(
			result.Errors,
		),
	)
	s.completeSystemPruneActivity(ctx, activityID, result)
	return result
}

func (s *Service) appendSystemPruneActivityMessage(ctx context.Context, activityID, message string, progress int) {
	if activityID == "" {
		return
	}
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    types.ActivityMessageLevelInfo,
		Message:  message,
		Progress: &progress,
		Step:     message,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append system prune activity message", "activityId", activityID, "error", err)
	}
}

func (s *Service) completeSystemPruneActivity(ctx context.Context, activityID string, result *types.PruneAllResult) {
	if activityID == "" {
		return
	}

	status, message, errMessage := func() (types.ActivityStatus, string, *string) {
		switch {
		case result.Success && len(result.Errors) == 0:
			return types.ActivityStatusSuccess, "System prune completed", nil
		case activitylib.CancelledByContext(ctx):
			return types.ActivityStatusCancelled, "System prune cancelled", nil
		default:
			return types.ActivityStatusFailed, "System prune completed with errors", new(strings.Join(result.Errors, "; "))
		}
	}()
	if _, err := s.activityService.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errMessage); err != nil {
		slog.DebugContext(ctx, "failed to complete system prune activity", "activityId", activityID, "error", err)
	}
}

func (s *Service) pruneContainers(ctx context.Context, options types.PruneContainersOptions) (types.PruneAllResult, error) {
	filterArgs := make(client.Filters)
	if options.Mode == types.PruneContainerModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return types.PruneAllResult{}, errors.New("container prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	}

	cli, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune containers: %w", err)
	}
	report, err := cli.ContainerPrune(ctx, client.ContainerPruneOptions{Filters: filterArgs})
	if err != nil {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune containers: %w", err)
	}

	return types.PruneAllResult{
		ContainersPruned:        report.Report.ContainersDeleted,
		SpaceReclaimed:          report.Report.SpaceReclaimed,
		ContainerSpaceReclaimed: report.Report.SpaceReclaimed,
	}, nil
}

func (s *Service) pruneImages(ctx context.Context, options types.PruneImagesOptions) (types.PruneAllResult, error) {
	filterArgs := make(client.Filters)
	switch options.Mode {
	case types.PruneImageModeNone:
		return types.PruneAllResult{}, errors.New("image prune mode none is not allowed")
	case types.PruneImageModeDangling:
		filterArgs = filterArgs.Add("dangling", "true")
	case types.PruneImageModeAll:
		filterArgs = filterArgs.Add("dangling", "false")
	case types.PruneImageModeOlderThan:
		if strings.TrimSpace(options.Until) == "" {
			return types.PruneAllResult{}, errors.New("image prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("dangling", "false")
		filterArgs = filterArgs.Add("until", options.Until)
	default:
		return types.PruneAllResult{}, fmt.Errorf("unsupported image prune mode: %s", options.Mode)
	}

	cli, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune images: %w", err)
	}
	existing, listErr := cli.ImageList(ctx, client.ImageListOptions{})
	if listErr != nil {
		slog.WarnContext(ctx, "Failed to list images before prune; deleted counts may include layers", "error", listErr)
	}
	existingIDs := make(map[string]struct{}, len(existing.Items))
	for _, item := range existing.Items {
		existingIDs[item.ID] = struct{}{}
	}

	report, err := cli.ImagePrune(ctx, client.ImagePruneOptions{Filters: filterArgs})
	if err != nil {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune images: %w", err)
	}

	var deletedImageIDs, untaggedRefs []string
	for _, imgReport := range report.Report.ImagesDeleted {
		_, isImage := existingIDs[imgReport.Deleted]
		switch {
		case imgReport.Deleted != "" && (isImage || listErr != nil):
			deletedImageIDs = append(deletedImageIDs, imgReport.Deleted)
		case imgReport.Untagged != "":
			untaggedRefs = append(untaggedRefs, imgReport.Untagged)
		}
	}
	slog.InfoContext(ctx, "Image pruning completed", "imagesDeleted", len(deletedImageIDs), "bytesReclaimed", report.Report.SpaceReclaimed)

	if deleteRecordsForImagesErr := s.imageUpdateService.DeleteRecordsForImages(ctx, slices.Concat(deletedImageIDs, untaggedRefs)); deleteRecordsForImagesErr != nil {
		slog.WarnContext(ctx, "Failed to delete image update records", "count", len(deletedImageIDs)+len(untaggedRefs), "error", deleteRecordsForImagesErr.Error())
	}

	return types.PruneAllResult{
		ImagesDeleted:       deletedImageIDs,
		SpaceReclaimed:      report.Report.SpaceReclaimed,
		ImageSpaceReclaimed: report.Report.SpaceReclaimed,
	}, nil
}

func (s *Service) pruneBuildCache(ctx context.Context, options types.PruneBuildCacheOptions) (types.PruneAllResult, error) {
	if options.Mode == types.PruneBuildCacheModeOlderThan && strings.TrimSpace(options.Until) == "" {
		return types.PruneAllResult{}, errors.New("build cache prune mode olderThan requires until")
	}

	slog.DebugContext(ctx, "starting build cache pruning", "mode", options.Mode, "until", options.Until)
	cli, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Error pruning build cache", "error", err.Error())
		return types.PruneAllResult{}, fmt.Errorf("failed to prune build cache: %w", err)
	}
	report, err := cli.BuildCachePrune(ctx, client.BuildCachePruneOptions{
		All:     options.Mode == types.PruneBuildCacheModeAll,
		Filters: kit.Ternary(options.Mode == types.PruneBuildCacheModeOlderThan, make(client.Filters).Add("until", options.Until), nil),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error pruning build cache", "error", err.Error())
		return types.PruneAllResult{}, fmt.Errorf("failed to prune build cache: %w", err)
	}

	slog.InfoContext(ctx, "build cache pruning completed", "cacheEntriesDeleted", len(report.Report.CachesDeleted), "bytesReclaimed", report.Report.SpaceReclaimed)

	return types.PruneAllResult{
		SpaceReclaimed:           report.Report.SpaceReclaimed,
		BuildCacheSpaceReclaimed: report.Report.SpaceReclaimed,
	}, nil
}

func (s *Service) pruneVolumes(ctx context.Context, options types.PruneVolumesOptions) (types.PruneAllResult, error) {
	plan, err := s.preparePrunePlan(ctx, types.PruneAllRequest{Volumes: &options})
	if err != nil {
		return types.PruneAllResult{}, err
	}
	result, err := s.executePrunePlan(ctx, plan, types.SchedulerRun{}, "", false)
	if err != nil {
		return types.PruneAllResult{}, err
	}
	if !result.Success {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune volumes: %s", strings.Join(result.Errors, "; "))
	}

	slog.InfoContext(ctx, "Volume prune completed", "volumesDeleted", len(result.VolumesDeleted), "spaceReclaimed", result.VolumeSpaceReclaimed)

	return types.PruneAllResult{
		VolumesDeleted:       result.VolumesDeleted,
		SpaceReclaimed:       result.VolumeSpaceReclaimed,
		VolumeSpaceReclaimed: result.VolumeSpaceReclaimed,
	}, nil
}

func (s *Service) pruneNetworks(ctx context.Context, options types.PruneNetworksOptions) (types.PruneAllResult, error) {
	filterArgs := make(client.Filters)
	if options.Mode == types.PruneNetworkModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return types.PruneAllResult{}, errors.New("network prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	}

	cli, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune networks: %w", err)
	}
	report, err := cli.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: filterArgs})
	if err != nil {
		return types.PruneAllResult{}, fmt.Errorf("failed to prune networks: %w", err)
	}

	slog.InfoContext(ctx, "Network prune completed", "networksDeleted", len(report.Report.NetworksDeleted))

	return types.PruneAllResult{NetworksDeleted: report.Report.NetworksDeleted}, nil
}

type pruneResource struct {
	Kind  string   `json:"kind"`
	ID    string   `json:"id"`
	Size  int64    `json:"size"`
	Names []string `json:"names,omitempty"`
}

type prunePlan struct {
	Request   types.PruneAllRequest `json:"request"`
	Resources []pruneResource       `json:"resources"`
}

// PruneScheduled freezes exact targets before applying destructive work.
func (s *Service) PruneScheduled(ctx context.Context, environmentID string, req types.PruneAllRequest) (*types.PruneAllResult, bool, error) {
	prune := s.beginSystemPrune(ctx, environmentID, req)
	if prune.started.IsAbsent() {
		return &types.PruneAllResult{
			Success:    true,
			ActivityID: mo.EmptyableToOption(prune.activityID).ToPointer(),
		}, false, nil
	}
	defer s.finishSystemPrune(environmentID)
	fail := func(err error) (*types.PruneAllResult, bool, error) {
		result := &types.PruneAllResult{
			Success:    false,
			Errors:     []string{err.Error()},
			ActivityID: mo.EmptyableToOption(prune.activityID).ToPointer(),
		}
		s.completeSystemPruneActivity(ctx, prune.activityID, result)
		return result, true, err
	}

	plan, err := s.preparePrunePlan(ctx, req)
	if err != nil {
		return fail(err)
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fail(err)
	}
	if progressErr := jobcontext.Progress(
		ctx,
		types.TargetOutcome{
			ResourceType: "prune_plan",
			ID:           "prune-plan",
			Status:       types.RunStatusSucceeded,
			RecoveryData: payload,
			ActivityID:   prune.activityID,
		},
	); progressErr != nil {
		return fail(progressErr)
	}
	previous, _ := jobcontext.Run(ctx)
	result, err := s.executePrunePlan(ctx, plan, previous, prune.activityID, false)
	s.completeSystemPruneActivity(ctx, prune.activityID, result)
	if err == nil && result.Success {
		err = jobcontext.Progress(ctx, types.TargetOutcome{ID: "scheduled-prune", Status: types.RunStatusSucceeded, ActivityID: prune.activityID})
	}
	return result, true, err
}

func (s *Service) preparePrunePlan(ctx context.Context, req types.PruneAllRequest) (prunePlan, error) {
	if req.Volumes != nil {
		if _, reapErr := s.volumeService.ReapIdleHelpers(ctx, 0); reapErr != nil {
			slog.WarnContext(ctx, "could not remove idle volume helpers before prune", "error", reapErr.Error())
		}
	}
	usage, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.DiskUsageResult, error) {
		return cli.DiskUsage(ctx, client.DiskUsageOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			BuildCache: req.BuildCache != nil,
			Verbose:    true,
		})
	})
	if err != nil {
		return prunePlan{}, err
	}
	containers, err := s.selectPruneContainers(req, usage)
	if err != nil {
		return prunePlan{}, err
	}
	selectedContainers, imageRefs, volumeRefs := pruneSelectedReferences(containers, usage)
	images, err := s.selectPruneImages(req, usage, imageRefs)
	if err != nil {
		return prunePlan{}, err
	}
	networks, err := s.selectPruneNetworks(ctx, req, selectedContainers)
	if err != nil {
		return prunePlan{}, err
	}
	buildCache, err := s.selectPruneBuildCache(req, usage)
	if err != nil {
		return prunePlan{}, err
	}
	return prunePlan{
		Request:   req,
		Resources: slices.Concat(containers, images, s.selectPruneVolumes(req, usage, volumeRefs), networks, buildCache),
	}, nil
}

func (s *Service) selectPruneContainers(req types.PruneAllRequest, usage client.DiskUsageResult) ([]pruneResource, error) {
	if req.Containers == nil || req.Containers.Mode == types.PruneContainerModeNone {
		return nil, nil
	}
	until, err := s.pruneCutoff(req.Containers.Mode == types.PruneContainerModeOlderThan, req.Containers.Until)
	if err != nil {
		return nil, err
	}
	var resources []pruneResource
	for _, item := range usage.Containers.Items {
		if item.State == "running" || item.State == "paused" || item.State == "restarting" || (!until.IsZero() && !time.Unix(item.Created, 0).Before(until)) {
			continue
		}
		resources = append(resources, pruneResource{
			Kind: "container",
			ID:   item.ID,
			Size: item.SizeRw,
		})
	}
	return resources, nil
}

func (s *Service) selectPruneImages(req types.PruneAllRequest, usage client.DiskUsageResult, imageRefs map[string]int) ([]pruneResource, error) {
	if req.Images == nil || req.Images.Mode == types.PruneImageModeNone {
		return nil, nil
	}
	until, err := s.pruneCutoff(req.Images.Mode == types.PruneImageModeOlderThan, req.Images.Until)
	if err != nil {
		return nil, err
	}
	var resources []pruneResource
	for _, item := range usage.Images.Items {
		if item.Containers > int64(imageRefs[item.ID]) ||
			(req.Images.Mode == types.PruneImageModeDangling && len(item.RepoTags) > 0) ||
			(!until.IsZero() && !time.Unix(item.Created, 0).Before(until)) {
			continue
		}
		resources = append(resources, pruneResource{
			Kind:  "image",
			ID:    item.ID,
			Size:  kit.Ternary(item.SharedSize >= 0, item.Size-item.SharedSize, 0),
			Names: slices.Clone(item.RepoTags),
		})
	}
	return resources, nil
}

func (s *Service) selectPruneVolumes(req types.PruneAllRequest, usage client.DiskUsageResult, volumeRefs map[string]int) []pruneResource {
	if req.Volumes == nil || req.Volumes.Mode == types.PruneVolumeModeNone {
		return nil
	}
	var resources []pruneResource
	for _, item := range usage.Volumes.Items {
		// The backup volume is unused between backups but holds them, so it is never pruned.
		if item.UsageData == nil || item.UsageData.RefCount > int64(volumeRefs[item.Name]) ||
			strings.EqualFold(item.Labels[libarcane.InternalResourceLabel], "true") || s.volumeService.IsBackupVolume(item.Name) {
			continue
		}
		if _, anonymous := item.Labels["com.docker.volume.anonymous"]; req.Volumes.Mode == types.PruneVolumeModeAnonymous && !anonymous {
			continue
		}
		resources = append(resources, pruneResource{
			Kind: "volume",
			ID:   item.Name,
			Size: item.UsageData.Size,
		})
	}
	return resources
}

func (s *Service) selectPruneNetworks(ctx context.Context, req types.PruneAllRequest, selectedContainers map[string]bool) ([]pruneResource, error) {
	if req.Networks == nil || req.Networks.Mode == types.PruneNetworkModeNone {
		return nil, nil
	}
	until, err := s.pruneCutoff(req.Networks.Mode == types.PruneNetworkModeOlderThan, req.Networks.Until)
	if err != nil {
		return nil, err
	}
	networks, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.NetworkListResult, error) {
		return cli.NetworkList(ctx, client.NetworkListOptions{})
	})
	if err != nil {
		return nil, err
	}
	var resources []pruneResource
nextNetwork:
	for _, item := range networks.Items {
		if item.Name == "bridge" || item.Name == "host" || item.Name == "none" || item.Ingress || item.Scope != "local" || (!until.IsZero() && !item.Created.Before(until)) {
			continue
		}
		inspect, networkInspectWithCompatibilityErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.NetworkInspectResult, error) {
			return compat.NetworkInspectWithCompatibility(ctx, cli, item.ID, client.NetworkInspectOptions{})
		})
		if networkInspectWithCompatibilityErr != nil {
			return nil, networkInspectWithCompatibilityErr
		}
		for id := range inspect.Network.Containers {
			if !selectedContainers[id] {
				continue nextNetwork
			}
		}
		resources = append(resources, pruneResource{
			Kind: "network",
			ID:   item.ID,
		})
	}
	return resources, nil
}

func (s *Service) selectPruneBuildCache(req types.PruneAllRequest, usage client.DiskUsageResult) ([]pruneResource, error) {
	if req.BuildCache == nil || req.BuildCache.Mode == types.PruneBuildCacheModeNone {
		return nil, nil
	}
	until, err := s.pruneCutoff(req.BuildCache.Mode == types.PruneBuildCacheModeOlderThan, req.BuildCache.Until)
	if err != nil {
		return nil, err
	}
	var resources []pruneResource
	for _, item := range usage.BuildCache.Items {
		if item.InUse || (!until.IsZero() && (item.LastUsedAt == nil || !item.LastUsedAt.Before(until))) {
			continue
		}
		resources = append(resources, pruneResource{
			Kind: "build_cache",
			ID:   item.ID,
			Size: item.Size,
		})
	}
	return resources, nil
}

func pruneSelectedReferences(containers []pruneResource, usage client.DiskUsageResult) (map[string]bool, map[string]int, map[string]int) {
	selectedContainers := make(map[string]bool)
	imageRefs := make(map[string]int)
	volumeRefs := make(map[string]int)
	for _, resource := range containers {
		selectedContainers[resource.ID] = true
	}
	for _, item := range usage.Containers.Items {
		if !selectedContainers[item.ID] {
			continue
		}
		imageRefs[item.ImageID]++
		mounted := make(map[string]bool)
		for _, m := range item.Mounts {
			if m.Type == "volume" && !mounted[m.Name] {
				mounted[m.Name] = true
				volumeRefs[m.Name]++
			}
		}
	}
	return selectedContainers, imageRefs, volumeRefs
}

func (s *Service) pruneCutoff(olderThan bool, raw string) (time.Time, error) {
	if !olderThan {
		return time.Time{}, nil
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration >= 0 {
		return time.Now().Add(-duration), nil
	}
	for _, format := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if value, err := time.ParseInLocation(format, raw, s.location); err == nil {
			return value, nil
		}
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		return time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*1e9)), nil
	}
	return time.Time{}, fmt.Errorf("unsupported prune cutoff %q", raw)
}

func (s *Service) executePrunePlan(
	ctx context.Context, plan prunePlan, previous types.SchedulerRun, activityID string, recovery bool,
) (*types.PruneAllResult, error) {
	var containersPruned, imagesDeleted, volumesDeleted, networksDeleted, errs []string
	var spaceReclaimed, containerSpace, imageSpace, volumeSpace, buildCacheSpace uint64
	finish := func(err error) (*types.PruneAllResult, error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
		return &types.PruneAllResult{
			ContainersPruned:         containersPruned,
			ImagesDeleted:            imagesDeleted,
			VolumesDeleted:           volumesDeleted,
			NetworksDeleted:          networksDeleted,
			SpaceReclaimed:           spaceReclaimed,
			ContainerSpaceReclaimed:  containerSpace,
			ImageSpaceReclaimed:      imageSpace,
			VolumeSpaceReclaimed:     volumeSpace,
			BuildCacheSpaceReclaimed: buildCacheSpace,
			Success:                  len(errs) == 0,
			Errors:                   errs,
			ActivityID:               mo.EmptyableToOption(activityID).ToPointer(),
		}, err
	}
	for _, resource := range plan.Resources {
		if resource.Kind == "build_cache" {
			continue
		}
		targetID := "prune:" + resource.Kind + ":" + resource.ID
		if slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
			return target.ID == targetID && (target.Status == types.RunStatusSucceeded || target.Status == types.RunStatusSkipped)
		}) {
			continue
		}
		if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
			ResourceType: resource.Kind,
			ID:           targetID,
			Status:       types.RunStatusRunning,
		}); progressErr != nil {
			return finish(progressErr)
		}
		size, eligible, exists, eligibleErr := s.pruneResourceEligible(ctx, plan, resource)
		if eligibleErr != nil {
			return finish(eligibleErr)
		}
		removeErr := func() error {
			if !exists || !eligible {
				return nil
			}
			return s.removePruneResource(ctx, resource)
		}()
		removeFailed := removeErr != nil && !errdefs.IsNotFound(removeErr)
		if removeFailed {
			errs = append(errs, removeErr.Error())
		}
		if exists && eligible && !removeFailed {
			// Something else removed it first; do not claim its bytes.
			claimed := kit.Ternary(removeErr == nil && size > 0, uint64(size), 0)
			spaceReclaimed += claimed
			switch resource.Kind {
			case "container":
				containersPruned = append(containersPruned, resource.ID)
				containerSpace += claimed
			case "image":
				imagesDeleted = append(imagesDeleted, resource.ID)
				imageSpace += claimed
			case "volume":
				volumesDeleted = append(volumesDeleted, resource.ID)
				volumeSpace += claimed
			case "network":
				networksDeleted = append(networksDeleted, resource.ID)
			}
		}
		if targetProgressErr := jobcontext.Progress(ctx, types.TargetOutcome{
			ResourceType: resource.Kind,
			ID:           targetID,
			Status: func() types.RunStatus {
				switch {
				case !exists:
					return types.RunStatusSucceeded
				case !eligible:
					return types.RunStatusSkipped
				case removeFailed:
					return types.RunStatusFailed
				default:
					return types.RunStatusSucceeded
				}
			}(),
			Message: kit.Ternary(removeFailed, fmt.Sprint(removeErr), ""),
		}); targetProgressErr != nil {
			return finish(targetProgressErr)
		}
	}
	if len(imagesDeleted) > 0 {
		if deleteRecordsForImagesErr := s.imageUpdateService.DeleteRecordsForImages(ctx, imagesDeleted); deleteRecordsForImagesErr != nil {
			return finish(deleteRecordsForImagesErr)
		}
	}
	reclaimed, err := s.executePruneCache(ctx, plan, recovery)
	spaceReclaimed += reclaimed
	buildCacheSpace += reclaimed
	return finish(err)
}

func (s *Service) removePruneResource(ctx context.Context, resource pruneResource) error {
	switch resource.Kind {
	case "container":
		_, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerRemoveResult, error) {
			return cli.ContainerRemove(ctx, resource.ID, client.ContainerRemoveOptions{})
		})
		return err
	case "image":
		for _, name := range resource.Names {
			inspected, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ImageInspectResult, error) {
				return cli.ImageInspect(ctx, name)
			})
			if errdefs.IsNotFound(inspectErr) {
				continue
			}
			if inspectErr != nil {
				return inspectErr
			}
			if inspected.ID != resource.ID {
				continue
			}
			if _, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ImageRemoveResult, error) {
				return cli.ImageRemove(ctx, name, client.ImageRemoveOptions{PruneChildren: false})
			}); err != nil {
				return err
			}
		}
		_, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ImageRemoveResult, error) {
			return cli.ImageRemove(ctx, resource.ID, client.ImageRemoveOptions{PruneChildren: false})
		})
		return err
	case "volume":
		return s.volumeService.DeleteVolume(ctx, resource.ID, false, types.SystemUser)
	case "network":
		return s.networkService.RemoveNetwork(ctx, resource.ID, types.SystemUser)
	default:
		return fmt.Errorf("unknown prune target %q", resource.Kind)
	}
}

func (s *Service) executePruneCache(ctx context.Context, plan prunePlan, recovery bool) (uint64, error) {
	request := plan.Request.BuildCache
	if request == nil || request.Mode == types.PruneBuildCacheModeNone {
		return 0, nil
	}
	if recovery {
		usage, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.DiskUsageResult, error) {
			return cli.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
		})
		if err != nil {
			return 0, err
		}
		selected := make(map[string]bool)
		for _, resource := range plan.Resources {
			if resource.Kind == "build_cache" {
				selected[resource.ID] = true
			}
		}
		for _, current := range usage.BuildCache.Items {
			if selected[current.ID] {
				return 0, errors.New("build cache prune outcome is unconfirmed")
			}
		}
		return 0, nil
	}
	var reclaimed uint64
	for _, resource := range plan.Resources {
		if resource.Kind != "build_cache" {
			continue
		}
		// Docker interprets the ID filter as a regular expression.
		filters := make(client.Filters).Add("id", "^"+regexp.QuoteMeta(resource.ID)+"$")
		if request.Mode == types.PruneBuildCacheModeOlderThan {
			filters = filters.Add("until", request.Until)
		}
		cli, err := s.dockerService.GetClient(ctx)
		if err != nil {
			return reclaimed, err
		}
		report, err := cli.BuildCachePrune(ctx, client.BuildCachePruneOptions{
			All:     request.Mode == types.PruneBuildCacheModeAll,
			Filters: filters,
		})
		if err != nil {
			return reclaimed, err
		}
		reclaimed += report.Report.SpaceReclaimed
	}
	return reclaimed, nil
}

// pruneResourceEligible re-checks a planned resource against live state and returns its size from that snapshot.
func (s *Service) pruneResourceEligible(ctx context.Context, plan prunePlan, resource pruneResource) (size int64, eligible, exists bool, err error) {
	if resource.Kind == "network" {
		inspected, networkInspectWithCompatibilityErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.NetworkInspectResult, error) {
			return compat.NetworkInspectWithCompatibility(ctx, cli, resource.ID, client.NetworkInspectOptions{})
		})
		if errdefs.IsNotFound(networkInspectWithCompatibilityErr) {
			return resource.Size, false, false, nil
		}
		if networkInspectWithCompatibilityErr != nil {
			return resource.Size, false, false, networkInspectWithCompatibilityErr
		}
		return resource.Size, len(inspected.Network.Containers) == 0 && !inspected.Network.Ingress, true, nil
	}
	switch resource.Kind {
	case "container":
		inspected, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, cli, resource.ID, client.ContainerInspectOptions{})
		})
		if errdefs.IsNotFound(inspectErr) {
			return resource.Size, false, false, nil
		}
		if inspectErr != nil {
			return resource.Size, false, false, inspectErr
		}
		state := kit.FromPtr(inspected.Container.State)
		return resource.Size, !state.Running && !state.Paused && !state.Restarting, true, nil
	case "image":
		inspected, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ImageInspectResult, error) {
			return cli.ImageInspect(ctx, resource.ID)
		})
		if errdefs.IsNotFound(inspectErr) {
			return resource.Size, false, false, nil
		}
		if inspectErr != nil {
			return resource.Size, false, false, inspectErr
		}
		if slices.ContainsFunc(inspected.RepoTags, func(tag string) bool { return !slices.Contains(resource.Names, tag) }) {
			return resource.Size, false, true, nil
		}
		users, listErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerListResult, error) {
			return cli.ContainerList(ctx, client.ContainerListOptions{
				All:     true,
				Filters: make(client.Filters).Add("ancestor", resource.ID),
			})
		})
		if listErr != nil {
			return resource.Size, false, true, listErr
		}
		return resource.Size, len(users.Items) == 0 && (plan.Request.Images.Mode != types.PruneImageModeDangling || len(inspected.RepoTags) == 0), true, nil
	case "volume":
		inspected, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.VolumeInspectResult, error) {
			return cli.VolumeInspect(ctx, resource.ID, client.VolumeInspectOptions{})
		})
		if errdefs.IsNotFound(inspectErr) {
			return resource.Size, false, false, nil
		}
		if inspectErr != nil {
			return resource.Size, false, false, inspectErr
		}
		users, usersErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) ([]string, error) {
			return docker.GetContainersUsingVolume(ctx, cli, resource.ID)
		})
		if usersErr != nil {
			return resource.Size, false, true, usersErr
		}
		return resource.Size, len(users) == 0 && !strings.EqualFold(inspected.Volume.Labels[libarcane.InternalResourceLabel], "true") &&
			!s.volumeService.IsBackupVolume(inspected.Volume.Name), true, nil
	default:
		return resource.Size, false, false, nil
	}
}

func (s *Service) ReconcileScheduledPrune(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, "scheduled-prune")
	if outcome.Status == types.RunStatusSucceeded {
		return outcome, nil
	}
	for _, target := range previous.Outcome.Targets {
		if target.ID != "prune-plan" || len(target.RecoveryData) == 0 {
			continue
		}
		var plan prunePlan
		if err := json.Unmarshal(target.RecoveryData, &plan); err != nil {
			return outcome, err
		}
		prune := s.beginSystemPrune(ctx, previous.EnvironmentID, plan.Request)
		if prune.started.IsAbsent() {
			return outcome, nil
		}
		defer s.finishSystemPrune(previous.EnvironmentID) //nolint:gocritic // The matching recovery target always returns before the loop advances.
		result, err := s.executePrunePlan(ctx, plan, previous, "", true)
		s.completeSystemPruneActivity(ctx, prune.activityID, result)
		if err != nil {
			return types.SchedulerOutcome{
				Status:     outcome.Status,
				Message:    err.Error(),
				ActivityID: outcome.ActivityID,
				Targets:    outcome.Targets,
				Steps:      outcome.Steps,
			}, nil
		}
		if !result.Success {
			return types.SchedulerOutcome{Status: types.RunStatusPartial, Targets: previous.Outcome.Targets}, nil
		}
		if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{ID: "scheduled-prune", Status: types.RunStatusSucceeded}); progressErr != nil {
			return outcome, progressErr
		}
		return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
	}
	return outcome, nil
}
