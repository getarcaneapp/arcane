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
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker/compat"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
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
	dockerClient       func(ctx context.Context) (*client.Client, error)
	activityService    *activity.ActivityService
	imageUpdateService *imageupdate.ImageUpdateService
	volumeService      *volume.VolumeService
	networkService     *network.NetworkService
	pruneMu            sync.Mutex
	runningPrunes      map[string]string
}

func NewService(
	location *time.Location,
	dockerClient func(ctx context.Context) (*client.Client, error),
	activityService *activity.ActivityService,
	imageUpdateService *imageupdate.ImageUpdateService,
	volumeService *volume.VolumeService,
	networkService *network.NetworkService,
) *Service {
	return &Service{
		location:           location,
		dockerClient:       dockerClient,
		activityService:    activityService,
		imageUpdateService: imageUpdateService,
		volumeService:      volumeService,
		networkService:     networkService,
		runningPrunes:      make(map[string]string),
	}
}

func (s *Service) PruneAll(ctx context.Context, environmentID string, req system.PruneAllRequest) (*system.PruneAllResult, bool, error) {
	slog.InfoContext(ctx, "Starting selective prune operation",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"buildCache", req.BuildCache,
	)

	prune := s.beginSystemPruneInternal(ctx, environmentID, req)
	activityID := prune.activityID
	result := &system.PruneAllResult{Success: true, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}
	if prune.started.IsAbsent() {
		slog.InfoContext(ctx, "System prune already running", "environmentId", environmentID, "activityId", activityID)
		return result, false, nil
	}

	defer s.finishSystemPruneInternal(environmentID)

	ctx = s.activityService.Track(ctx, activityID)
	if err := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: "scheduled-prune", Status: scheduler.Running, ActivityID: activityID}); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err.Error())
		s.completeSystemPruneActivityInternal(ctx, activityID, result)
		return result, true, err
	}
	s.runSystemPruneInternal(ctx, req, activityID, result)

	return result, true, nil
}

func (s *Service) StartPruneAll(ctx context.Context, environmentID string, req system.PruneAllRequest) *system.PruneAllResult {
	prune := s.beginSystemPruneInternal(ctx, environmentID, req)
	activityID := prune.activityID
	if prune.started.IsAbsent() {
		slog.InfoContext(ctx, "System prune already running", "environmentId", environmentID, "activityId", activityID)
		return &system.PruneAllResult{Success: true, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}
	}

	backgroundCtx := utils.ActivityRuntimeContext(ctx, nil)
	backgroundCtx = s.activityService.Track(backgroundCtx, activityID)

	go func() {
		defer s.finishSystemPruneInternal(environmentID)

		result := &system.PruneAllResult{Success: true, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}
		s.runSystemPruneInternal(backgroundCtx, req, activityID, result)
	}()

	return &system.PruneAllResult{Success: true, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}
}

type systemPruneBeginResult struct {
	activityID string
	started    mo.Option[struct{}]
}

func (s *Service) beginSystemPruneInternal(ctx context.Context, environmentID string, req system.PruneAllRequest) systemPruneBeginResult {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	if activityID, ok := s.runningPrunes[environmentID]; ok {
		return systemPruneBeginResult{activityID: activityID, started: mo.None[struct{}]()}
	}

	activityID := s.startSystemPruneActivityInternal(ctx, environmentID, req)
	s.runningPrunes[environmentID] = activityID
	return systemPruneBeginResult{activityID: activityID, started: mo.Some(struct{}{})}
}

func (s *Service) finishSystemPruneInternal(environmentID string) {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	delete(s.runningPrunes, environmentID)
}

func (s *Service) runSystemPruneInternal(ctx context.Context, req system.PruneAllRequest, activityID string, result *system.PruneAllResult) {
	var mu sync.Mutex

	// 1. Prune Containers first (sequential) as it may free up other resources
	if req.Containers != nil && req.Containers.Mode != system.PruneContainerModeNone {
		s.appendSystemPruneActivityMessageInternal(ctx, activityID, "Pruning containers", 15)
		slog.InfoContext(ctx, "Pruning containers...", "mode", req.Containers.Mode, "until", req.Containers.Until)
		if err := s.pruneContainersInternal(ctx, *req.Containers, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Container pruning failed: %v", err))
			result.Success = false
		}
	}

	// 2. Prune other resources in parallel
	g, groupCtx := errgroup.WithContext(ctx)

	if req.Images != nil && req.Images.Mode != system.PruneImageModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessageInternal(groupCtx, activityID, "Pruning images", 40)
			slog.InfoContext(groupCtx, "Pruning images...", "mode", req.Images.Mode, "until", req.Images.Until)
			localResult := &system.PruneAllResult{}
			if err := s.pruneImagesInternal(groupCtx, *req.Images, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Image pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.ImagesDeleted = append(result.ImagesDeleted, localResult.ImagesDeleted...)
				result.SpaceReclaimed += localResult.SpaceReclaimed
				result.ImageSpaceReclaimed += localResult.ImageSpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.BuildCache != nil && req.BuildCache.Mode != system.PruneBuildCacheModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessageInternal(groupCtx, activityID, "Pruning build cache", 45)
			slog.InfoContext(groupCtx, "Pruning build cache...", "mode", req.BuildCache.Mode, "until", req.BuildCache.Until)
			localResult := &system.PruneAllResult{}
			if err := s.pruneBuildCacheInternal(groupCtx, *req.BuildCache, localResult); err != nil {
				slog.WarnContext(groupCtx, "Build cache pruning encountered an error", "error", err.Error())
				// Surface the failure like every other prune type so a build cache that
				// could not be reclaimed is reported instead of silently left behind.
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Build cache pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.SpaceReclaimed += localResult.SpaceReclaimed
				result.BuildCacheSpaceReclaimed += localResult.BuildCacheSpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.Volumes != nil && req.Volumes.Mode != system.PruneVolumeModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessageInternal(groupCtx, activityID, "Pruning volumes", 55)
			slog.InfoContext(groupCtx, "Pruning volumes...", "mode", req.Volumes.Mode)
			localResult := &system.PruneAllResult{}
			if err := s.pruneVolumesInternal(groupCtx, *req.Volumes, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Volume pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.VolumesDeleted = append(result.VolumesDeleted, localResult.VolumesDeleted...)
				result.SpaceReclaimed += localResult.SpaceReclaimed
				result.VolumeSpaceReclaimed += localResult.VolumeSpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.Networks != nil && req.Networks.Mode != system.PruneNetworkModeNone {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "system info worker")

			s.appendSystemPruneActivityMessageInternal(groupCtx, activityID, "Pruning networks", 65)
			slog.InfoContext(groupCtx, "Pruning networks...", "mode", req.Networks.Mode, "until", req.Networks.Until)
			localResult := &system.PruneAllResult{}
			if err := s.pruneNetworksInternal(groupCtx, *req.Networks, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Network pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.NetworksDeleted = append(result.NetworksDeleted, localResult.NetworksDeleted...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "Prune operations failed", "error", err)
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
	s.completeSystemPruneActivityInternal(ctx, activityID, result)
}

func (s *Service) startSystemPruneActivityInternal(ctx context.Context, environmentID string, req system.PruneAllRequest) string {
	if s.activityService == nil {
		return ""
	}
	localActivity, err := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
		EnvironmentID: environmentID,
		Type:          activitytypes.TypeSystemPrune,
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
		return ""
	}
	return localActivity.ID
}

func (s *Service) appendSystemPruneActivityMessageInternal(ctx context.Context, activityID, message string, progress int) {
	if s.activityService == nil || activityID == "" {
		return
	}
	if _, err := s.activityService.AppendMessage(ctx, activityID, activity.AppendActivityMessageRequest{
		Level:    activitytypes.MessageLevelInfo,
		Message:  message,
		Progress: &progress,
		Step:     message,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append system prune activity message", "activityId", activityID, "error", err)
	}
}

func (s *Service) completeSystemPruneActivityInternal(ctx context.Context, activityID string, result *system.PruneAllResult) {
	if s.activityService == nil || activityID == "" || result == nil {
		return
	}

	status := activitytypes.StatusSuccess
	message := "System prune completed"
	var errMessage *string
	if !result.Success || len(result.Errors) > 0 {
		if activitylib.CancelledByContext(ctx) {
			status = activitytypes.StatusCancelled
			message = "System prune cancelled"
		} else {
			status = activitytypes.StatusFailed
			message = "System prune completed with errors"
			errMessage = new(strings.Join(result.Errors, "; "))
		}
	}
	if _, err := s.activityService.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errMessage); err != nil {
		slog.DebugContext(ctx, "failed to complete system prune activity", "activityId", activityID, "error", err)
	}
}

func (s *Service) pruneContainersInternal(ctx context.Context, options system.PruneContainersOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	if options.Mode == system.PruneContainerModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return errors.New("container prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	}

	report, err := dockerClient.ContainerPrune(ctx, client.ContainerPruneOptions{Filters: filterArgs})
	if err != nil {
		return fmt.Errorf("failed to prune containers: %w", err)
	}

	result.ContainersPruned = report.Report.ContainersDeleted
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	result.ContainerSpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func (s *Service) pruneImagesInternal(ctx context.Context, options system.PruneImagesOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	switch options.Mode {
	case system.PruneImageModeNone:
		return errors.New("image prune mode none is not allowed")
	case system.PruneImageModeDangling:
		filterArgs = filterArgs.Add("dangling", "true")
	case system.PruneImageModeAll:
		filterArgs = filterArgs.Add("dangling", "false")
	case system.PruneImageModeOlderThan:
		if strings.TrimSpace(options.Until) == "" {
			return errors.New("image prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("dangling", "false")
		filterArgs = filterArgs.Add("until", options.Until)
	default:
		return fmt.Errorf("unsupported image prune mode: %s", options.Mode)
	}

	report, err := dockerClient.ImagePrune(ctx, client.ImagePruneOptions{Filters: filterArgs})
	if err != nil {
		return fmt.Errorf("failed to prune images: %w", err)
	}

	slog.InfoContext(ctx, "Image pruning completed", "imagesDeleted", len(report.Report.ImagesDeleted), "bytesReclaimed", report.Report.SpaceReclaimed)

	// Collect IDs to delete from DB
	var idsToDelete []string
	for _, imgReport := range report.Report.ImagesDeleted {
		if imgReport.Deleted != "" {
			idsToDelete = append(idsToDelete, imgReport.Deleted)
		} else if imgReport.Untagged != "" {
			idsToDelete = append(idsToDelete, imgReport.Untagged)
		}
	}

	if deleteRecordsForImagesErr := s.imageUpdateService.DeleteRecordsForImages(ctx, idsToDelete); deleteRecordsForImagesErr != nil {
		slog.WarnContext(ctx, "Failed to delete image update records", "count", len(idsToDelete), "error", deleteRecordsForImagesErr.Error())
	}

	result.ImagesDeleted = idsToDelete
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	result.ImageSpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func (s *Service) pruneBuildCacheInternal(ctx context.Context, options system.PruneBuildCacheOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("build cache pruning failed (connection): %w", err).Error())
		slog.ErrorContext(ctx, "Error connecting to Docker for build cache prune", "error", err.Error())
		return fmt.Errorf("failed to connect to Docker for build cache prune: %w", err)
	}

	pruneOptions := client.BuildCachePruneOptions{
		All: options.Mode == system.PruneBuildCacheModeAll,
	}
	if options.Mode == system.PruneBuildCacheModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return errors.New("build cache prune mode olderThan requires until")
		}
		pruneOptions.Filters = make(client.Filters)
		pruneOptions.Filters = pruneOptions.Filters.Add("until", options.Until)
	}

	slog.DebugContext(ctx, "starting build cache pruning", "mode", options.Mode, "until", options.Until)
	report, err := dockerClient.BuildCachePrune(ctx, pruneOptions)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("build cache pruning failed: %w", err).Error())
		slog.ErrorContext(ctx, "Error pruning build cache", "error", err.Error())
		return fmt.Errorf("failed to prune build cache: %w", err)
	}

	slog.InfoContext(ctx, "build cache pruning completed", "cacheEntriesDeleted", len(report.Report.CachesDeleted), "bytesReclaimed", report.Report.SpaceReclaimed)

	result.SpaceReclaimed += report.Report.SpaceReclaimed
	result.BuildCacheSpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func (s *Service) pruneVolumesInternal(ctx context.Context, options system.PruneVolumesOptions, result *system.PruneAllResult) error {
	allVolumes := options.Mode == system.PruneVolumeModeAll
	report, err := s.volumeService.PruneVolumesWithOptions(ctx, allVolumes)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Volume prune completed", "volumesDeleted", len(report.VolumesDeleted), "spaceReclaimed", report.SpaceReclaimed)

	result.VolumesDeleted = report.VolumesDeleted
	result.SpaceReclaimed += report.SpaceReclaimed
	result.VolumeSpaceReclaimed += report.SpaceReclaimed
	return nil
}

func (s *Service) pruneNetworksInternal(ctx context.Context, options system.PruneNetworksOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	if options.Mode == system.PruneNetworkModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return errors.New("network prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	}

	report, err := dockerClient.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: filterArgs})
	if err != nil {
		return fmt.Errorf("failed to prune networks: %w", err)
	}

	slog.InfoContext(ctx, "Network prune completed", "networksDeleted", len(report.Report.NetworksDeleted))

	result.NetworksDeleted = report.Report.NetworksDeleted
	return nil
}

type pruneResourceInternal struct {
	Kind  string   `json:"kind"`
	ID    string   `json:"id"`
	Size  int64    `json:"size"`
	Names []string `json:"names,omitempty"`
}

type prunePlanInternal struct {
	Request   system.PruneAllRequest  `json:"request"`
	Resources []pruneResourceInternal `json:"resources"`
}

// PruneScheduled freezes exact targets before applying destructive work.
func (s *Service) PruneScheduled(ctx context.Context, environmentID string, req system.PruneAllRequest) (*system.PruneAllResult, bool, error) {
	prune := s.beginSystemPruneInternal(ctx, environmentID, req)
	result := &system.PruneAllResult{Success: true}
	if prune.activityID != "" {
		result.ActivityID = &prune.activityID
	}
	if prune.started.IsAbsent() {
		return result, false, nil
	}
	defer s.finishSystemPruneInternal(environmentID)
	fail := func(err error) (*system.PruneAllResult, bool, error) {
		result.Success = false
		result.Errors = append(result.Errors, err.Error())
		s.completeSystemPruneActivityInternal(ctx, prune.activityID, result)
		return result, true, err
	}

	plan, err := s.preparePrunePlanInternal(ctx, req)
	if err != nil {
		return fail(err)
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return fail(err)
	}
	if progressErr := jobcontext.Progress(
		ctx,
		scheduler.TargetOutcome{
			ResourceType: "prune_plan",
			ID:           "prune-plan",
			Status:       scheduler.Succeeded,
			RecoveryData: payload,
			ActivityID:   prune.activityID,
		},
	); progressErr != nil {
		return fail(progressErr)
	}
	previous, _ := jobcontext.Run(ctx)
	err = s.executePrunePlanInternal(ctx, plan, previous, result, false)
	s.completeSystemPruneActivityInternal(ctx, prune.activityID, result)
	if err == nil && result.Success {
		err = jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: "scheduled-prune", Status: scheduler.Succeeded, ActivityID: prune.activityID})
	}
	return result, true, err
}

func (s *Service) preparePrunePlanInternal(ctx context.Context, req system.PruneAllRequest) (prunePlanInternal, error) {
	plan := prunePlanInternal{Request: req}
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return plan, err
	}
	if req.Volumes != nil {
		s.volumeService.CleanupHelperContainers(ctx)
	}
	usage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{Containers: true, Images: true, Volumes: true, BuildCache: req.BuildCache != nil, Verbose: true})
	if err != nil {
		return plan, err
	}
	if selectPruneContainersErr := s.selectPruneContainersInternal(&plan, usage); selectPruneContainersErr != nil {
		return plan, selectPruneContainersErr
	}
	selectedContainers, imageRefs, volumeRefs := pruneSelectedReferencesInternal(plan.Resources, usage)
	if selectPruneImagesErr := s.selectPruneImagesInternal(&plan, usage, imageRefs); selectPruneImagesErr != nil {
		return plan, selectPruneImagesErr
	}
	selectPruneVolumesInternal(&plan, usage, volumeRefs)
	if selectPruneNetworksErr := s.selectPruneNetworksInternal(ctx, dockerClient, &plan, selectedContainers); selectPruneNetworksErr != nil {
		return plan, selectPruneNetworksErr
	}
	if selectPruneBuildCacheErr := s.selectPruneBuildCacheInternal(&plan, usage); selectPruneBuildCacheErr != nil {
		return plan, selectPruneBuildCacheErr
	}
	return plan, nil
}

func (s *Service) selectPruneContainersInternal(plan *prunePlanInternal, usage client.DiskUsageResult) error {
	req := plan.Request
	var err error
	if req.Containers == nil || req.Containers.Mode == system.PruneContainerModeNone {
		return nil
	}
	until := time.Time{}
	if req.Containers.Mode == system.PruneContainerModeOlderThan {
		until, err = s.pruneCutoffInternal(req.Containers.Until)
		if err != nil {
			return err
		}
	}
	for _, item := range usage.Containers.Items {
		if item.State == "running" || item.State == "paused" || item.State == "restarting" || (!until.IsZero() && !time.Unix(item.Created, 0).Before(until)) {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "container", ID: item.ID, Size: item.SizeRw})
	}
	return nil
}

func (s *Service) selectPruneImagesInternal(plan *prunePlanInternal, usage client.DiskUsageResult, imageRefs map[string]int) error {
	req := plan.Request
	var err error
	if req.Images == nil || req.Images.Mode == system.PruneImageModeNone {
		return nil
	}
	until := time.Time{}
	if req.Images.Mode == system.PruneImageModeOlderThan {
		until, err = s.pruneCutoffInternal(req.Images.Until)
		if err != nil {
			return err
		}
	}
	for _, item := range usage.Images.Items {
		if item.Containers > int64(
			imageRefs[item.ID],
		) || (req.Images.Mode == system.PruneImageModeDangling && len(
			item.RepoTags,
		) > 0) || (!until.IsZero() && !time.Unix(
			item.Created,
			0,
		).Before(
			until,
		)) {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "image", ID: item.ID, Size: item.Size, Names: slices.Clone(item.RepoTags)})
	}
	return nil
}

func selectPruneVolumesInternal(plan *prunePlanInternal, usage client.DiskUsageResult, volumeRefs map[string]int) {
	req := plan.Request
	if req.Volumes != nil && req.Volumes.Mode != system.PruneVolumeModeNone {
		for _, item := range usage.Volumes.Items {
			if item.UsageData == nil || item.UsageData.RefCount > int64(volumeRefs[item.Name]) || strings.EqualFold(item.Labels[libarcane.InternalResourceLabel], "true") {
				continue
			}
			if req.Volumes.Mode == system.PruneVolumeModeAnonymous {
				if _, anonymous := item.Labels["com.docker.volume.anonymous"]; !anonymous {
					continue
				}
			}
			plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "volume", ID: item.Name, Size: item.UsageData.Size})
		}
	}
}

func (s *Service) selectPruneNetworksInternal(ctx context.Context, dockerClient *client.Client, plan *prunePlanInternal, selectedContainers map[string]bool) error {
	req := plan.Request
	var err error
	if req.Networks == nil || req.Networks.Mode == system.PruneNetworkModeNone {
		return nil
	}
	until := time.Time{}
	if req.Networks.Mode == system.PruneNetworkModeOlderThan {
		until, err = s.pruneCutoffInternal(req.Networks.Until)
		if err != nil {
			return err
		}
	}
	networks, err := dockerClient.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return err
	}
	for _, item := range networks.Items {
		if item.Name == "bridge" || item.Name == "host" || item.Name == "none" || item.Ingress || item.Scope != "local" || (!until.IsZero() && !item.Created.Before(until)) {
			continue
		}
		inspect, networkInspectWithCompatibilityErr := compat.NetworkInspectWithCompatibility(ctx, dockerClient, item.ID, client.NetworkInspectOptions{})
		if networkInspectWithCompatibilityErr != nil {
			return networkInspectWithCompatibilityErr
		}
		eligible := true
		for id := range inspect.Network.Containers {
			if !selectedContainers[id] {
				eligible = false
				break
			}
		}
		if !eligible {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "network", ID: item.ID})
	}
	return nil
}

func (s *Service) selectPruneBuildCacheInternal(plan *prunePlanInternal, usage client.DiskUsageResult) error {
	req := plan.Request
	var err error
	if req.BuildCache == nil || req.BuildCache.Mode == system.PruneBuildCacheModeNone {
		return nil
	}
	until := time.Time{}
	if req.BuildCache.Mode == system.PruneBuildCacheModeOlderThan {
		until, err = s.pruneCutoffInternal(req.BuildCache.Until)
		if err != nil {
			return err
		}
	}
	for _, item := range usage.BuildCache.Items {
		if item.InUse || (!until.IsZero() && (item.LastUsedAt == nil || !item.LastUsedAt.Before(until))) {
			continue
		}
		plan.Resources = append(plan.Resources, pruneResourceInternal{Kind: "build_cache", ID: item.ID, Size: item.Size})
	}
	return nil
}

func pruneSelectedReferencesInternal(resources []pruneResourceInternal, usage client.DiskUsageResult) (map[string]bool, map[string]int, map[string]int) {
	selectedContainers := make(map[string]bool)
	imageRefs := make(map[string]int)
	volumeRefs := make(map[string]int)
	for _, resource := range resources {
		if resource.Kind == "container" {
			selectedContainers[resource.ID] = true
		}
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

func (s *Service) pruneCutoffInternal(raw string) (time.Time, error) {
	now := time.Now()
	location := s.location
	if location == nil {
		location = time.UTC
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration >= 0 {
		return now.Add(-duration), nil
	}
	for _, format := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if value, err := time.ParseInLocation(format, raw, location); err == nil {
			return value, nil
		}
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		return time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*1e9)), nil
	}
	return time.Time{}, fmt.Errorf("unsupported prune cutoff %q", raw)
}

func (s *Service) executePrunePlanInternal(ctx context.Context, plan prunePlanInternal, previous scheduler.Run, result *system.PruneAllResult, recovery bool) error {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return err
	}
	for _, resource := range plan.Resources {
		if resource.Kind == "build_cache" {
			continue
		}
		target := scheduler.TargetOutcome{ResourceType: resource.Kind, ID: "prune:" + resource.Kind + ":" + resource.ID, Status: scheduler.Running}
		if pruneTargetCompletedInternal(previous, target.ID) {
			continue
		}
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return progressErr
		}
		eligible, exists, pruneResourceEligibleErr := s.pruneResourceEligibleInternal(ctx, dockerClient, plan, &resource)
		if pruneResourceEligibleErr != nil {
			return pruneResourceEligibleErr
		}
		switch {
		case !exists:
			target.Status = scheduler.Succeeded
		case !eligible:
			target.Status = scheduler.Skipped
		default:
			pruneResourceEligibleErr = s.removePruneResourceInternal(ctx, dockerClient, resource)
			if pruneResourceEligibleErr != nil && !errdefs.IsNotFound(pruneResourceEligibleErr) {
				target.Status = scheduler.Failed
				target.Message = pruneResourceEligibleErr.Error()
				result.Success = false
				result.Errors = append(result.Errors, pruneResourceEligibleErr.Error())
			} else {
				target.Status = scheduler.Succeeded
				if pruneResourceEligibleErr != nil {
					// Something else removed it first; do not claim its bytes.
					resource.Size = 0
				}
				recordPrunedResourceInternal(resource, result)
			}
		}
		if targetProgressErr := jobcontext.Progress(ctx, target); targetProgressErr != nil {
			return targetProgressErr
		}
	}
	if len(result.ImagesDeleted) > 0 {
		if deleteRecordsForImagesErr := s.imageUpdateService.DeleteRecordsForImages(ctx, result.ImagesDeleted); deleteRecordsForImagesErr != nil {
			return deleteRecordsForImagesErr
		}
	}
	return s.executePruneCacheInternal(ctx, dockerClient, plan, result, recovery)
}

func (s *Service) removePruneResourceInternal(ctx context.Context, dockerClient *client.Client, resource pruneResourceInternal) error {
	var err error
	switch resource.Kind {
	case "container":
		_, err = dockerClient.ContainerRemove(ctx, resource.ID, client.ContainerRemoveOptions{})
	case "image":
		err = removePruneImageInternal(ctx, dockerClient, resource)
	case "volume":
		err = s.volumeService.DeleteVolume(ctx, resource.ID, false, user.SystemUser)
	case "network":
		err = s.networkService.RemoveNetwork(ctx, resource.ID, user.SystemUser)
	default:
		return fmt.Errorf("unknown prune target %q", resource.Kind)
	}
	return err
}

func removePruneImageInternal(ctx context.Context, dockerClient *client.Client, resource pruneResourceInternal) error {
	var err error
	for _, name := range resource.Names {
		inspected, inspectErr := dockerClient.ImageInspect(ctx, name)
		if errdefs.IsNotFound(inspectErr) {
			continue
		}
		if inspectErr != nil {
			return inspectErr
		}
		if inspected.ID != resource.ID {
			continue
		}
		_, err = dockerClient.ImageRemove(ctx, name, client.ImageRemoveOptions{PruneChildren: false})
		if err != nil {
			return err
		}
	}
	_, err = dockerClient.ImageRemove(ctx, resource.ID, client.ImageRemoveOptions{PruneChildren: false})
	return err
}

func recordPrunedResourceInternal(resource pruneResourceInternal, result *system.PruneAllResult) {
	switch resource.Kind {
	case "container":
		result.ContainersPruned = append(result.ContainersPruned, resource.ID)
	case "image":
		result.ImagesDeleted = append(result.ImagesDeleted, resource.ID)
	case "volume":
		result.VolumesDeleted = append(result.VolumesDeleted, resource.ID)
	case "network":
		result.NetworksDeleted = append(result.NetworksDeleted, resource.ID)
	}
	if resource.Size <= 0 {
		return
	}
	size := uint64(resource.Size)
	result.SpaceReclaimed += size
	switch resource.Kind {
	case "container":
		result.ContainerSpaceReclaimed += size
	case "image":
		result.ImageSpaceReclaimed += size
	case "volume":
		result.VolumeSpaceReclaimed += size
	}
}

func (s *Service) executePruneCacheInternal(ctx context.Context, dockerClient *client.Client, plan prunePlanInternal, result *system.PruneAllResult, recovery bool) error {
	request := plan.Request.BuildCache
	if request == nil || request.Mode == system.PruneBuildCacheModeNone {
		return nil
	}
	if recovery {
		usage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
		if err != nil {
			return err
		}
		selected := make(map[string]bool)
		for _, resource := range plan.Resources {
			if resource.Kind == "build_cache" {
				selected[resource.ID] = true
			}
		}
		for _, current := range usage.BuildCache.Items {
			if selected[current.ID] {
				return errors.New("build cache prune outcome is unconfirmed")
			}
		}
		return nil
	}
	for _, resource := range plan.Resources {
		if resource.Kind != "build_cache" {
			continue
		}
		// Docker interprets the ID filter as a regular expression.
		filters := make(client.Filters).Add("id", "^"+regexp.QuoteMeta(resource.ID)+"$")
		if request.Mode == system.PruneBuildCacheModeOlderThan {
			filters = filters.Add("until", request.Until)
		}
		report, err := dockerClient.BuildCachePrune(ctx, client.BuildCachePruneOptions{
			All:     request.Mode == system.PruneBuildCacheModeAll,
			Filters: filters,
		})
		if err != nil {
			return err
		}
		result.SpaceReclaimed += report.Report.SpaceReclaimed
		result.BuildCacheSpaceReclaimed += report.Report.SpaceReclaimed
	}
	return nil
}

// pruneResourceEligibleInternal re-checks a planned resource against live state and refreshes its size from that snapshot.
func (s *Service) pruneResourceEligibleInternal(ctx context.Context, dockerClient *client.Client, plan prunePlanInternal, resource *pruneResourceInternal) (eligible, exists bool, err error) {
	if resource.Kind == "network" {
		inspected, networkInspectWithCompatibilityErr := compat.NetworkInspectWithCompatibility(ctx, dockerClient, resource.ID, client.NetworkInspectOptions{})
		if errdefs.IsNotFound(networkInspectWithCompatibilityErr) {
			return false, false, nil
		}
		if networkInspectWithCompatibilityErr != nil {
			return false, false, networkInspectWithCompatibilityErr
		}
		return len(inspected.Network.Containers) == 0 && !inspected.Network.Ingress, true, nil
	}
	usage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{Containers: resource.Kind == "container", Images: resource.Kind == "image", Volumes: resource.Kind == "volume", Verbose: true})
	if err != nil {
		return false, false, err
	}
	switch resource.Kind {
	case "container":
		for _, item := range usage.Containers.Items {
			if item.ID == resource.ID {
				resource.Size = item.SizeRw
				return item.State != "running" && item.State != "paused" && item.State != "restarting", true, nil
			}
		}
	case "image":
		return pruneImageEligibleInternal(usage, plan.Request.Images, resource)
	case "volume":
		for _, item := range usage.Volumes.Items {
			if item.Name == resource.ID {
				if item.UsageData == nil {
					return false, true, nil
				}
				resource.Size = item.UsageData.Size
				return item.UsageData.RefCount == 0 && !strings.EqualFold(item.Labels[libarcane.InternalResourceLabel], "true"), true, nil
			}
		}
	}
	return false, false, nil
}

func pruneImageEligibleInternal(usage client.DiskUsageResult, request *system.PruneImagesOptions, resource *pruneResourceInternal) (eligible, exists bool, err error) {
	for _, item := range usage.Images.Items {
		if item.ID == resource.ID {
			// Size - SharedSize is the unique layer size Docker counts as reclaimable for an unused image.
			resource.Size = 0
			if item.SharedSize >= 0 {
				resource.Size = item.Size - item.SharedSize
			}
			for _, tag := range item.RepoTags {
				if !slices.Contains(resource.Names, tag) {
					return false, true, nil
				}
			}
			return item.Containers == 0 && (request.Mode != system.PruneImageModeDangling || len(item.RepoTags) == 0), true, nil
		}
	}
	return false, false, nil
}

func (s *Service) ReconcileScheduledPrune(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
	outcome := jobcontext.ConfirmedTarget(previous, "scheduled-prune")
	if outcome.Status == scheduler.Succeeded {
		return outcome, nil
	}
	for _, target := range previous.Outcome.Targets {
		if target.ID != "prune-plan" || len(target.RecoveryData) == 0 {
			continue
		}
		var plan prunePlanInternal
		if err := json.Unmarshal(target.RecoveryData, &plan); err != nil {
			return outcome, err
		}
		prune := s.beginSystemPruneInternal(ctx, previous.EnvironmentID, plan.Request)
		if prune.started.IsAbsent() {
			return outcome, nil
		}
		defer s.finishSystemPruneInternal(previous.EnvironmentID) //nolint:gocritic // The matching recovery target always returns before the loop advances.
		result := &system.PruneAllResult{Success: true}
		err := s.executePrunePlanInternal(ctx, plan, previous, result, true)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, err.Error())
		}
		s.completeSystemPruneActivityInternal(ctx, prune.activityID, result)
		if err != nil {
			outcome.Message = err.Error()
			return outcome, nil
		}
		if !result.Success {
			return scheduler.Outcome{Status: scheduler.Partial, Targets: previous.Outcome.Targets}, nil
		}
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: "scheduled-prune", Status: scheduler.Succeeded}); progressErr != nil {
			return outcome, progressErr
		}
		return scheduler.Outcome{Status: scheduler.Succeeded}, nil
	}
	return outcome, nil
}

func pruneTargetCompletedInternal(previous scheduler.Run, targetID string) bool {
	for _, target := range previous.Outcome.Targets {
		if target.ID == targetID && (target.Status == scheduler.Succeeded || target.Status == scheduler.Skipped) {
			return true
		}
	}
	return false
}
