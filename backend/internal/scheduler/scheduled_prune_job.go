package scheduler

import (
	"context"
	"log/slog"

	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/notification"
	scheduleutil "getarcane.dev/arcane/v3/internal/scheduler/schedule"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	ScheduledPruneJob struct {
		dockerService       *dockerInternal.DockerClientService
		systemService       *system.SystemService
		settingsService     *settings.SettingsService
		notificationService *notification.NotificationService
	}
)

const (
	ScheduledPruneJobName = "scheduled-prune"
)

func NewScheduledPruneJob(
	systemService *system.SystemService,
	settingsService *settings.SettingsService,
	notificationService *notification.NotificationService,
	dockerService *dockerInternal.DockerClientService,
) *ScheduledPruneJob {
	return &ScheduledPruneJob{
		dockerService:       dockerService,
		systemService:       systemService,
		settingsService:     settingsService,
		notificationService: notificationService,
	}
}

func (j *ScheduledPruneJob) Name() string {
	return ScheduledPruneJobName
}

func (j *ScheduledPruneJob) ShouldSchedule(ctx context.Context) bool {
	return j.dockerService.LocalDockerEnabled() && j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled")
}

func (j *ScheduledPruneJob) Schedule(ctx context.Context) string {
	return scheduleutil.Or(ctx, j.settingsService.GetStringSetting(ctx, "scheduledPruneInterval"), "0 0 * * *", "scheduled-prune")
}

func (j *ScheduledPruneJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled")
	if !enabled {
		slog.DebugContext(ctx, "scheduled prune disabled; skipping run")
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}

	req := buildScheduledPruneRequest(ctx, j.settingsService)

	if req.Containers == nil && req.Images == nil && req.Volumes == nil && req.Networks == nil && req.BuildCache == nil {
		slog.InfoContext(ctx, "scheduled prune run skipped; no resource types selected")
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}

	slog.InfoContext(ctx, "scheduled prune run started",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"buildCache", req.BuildCache,
	)

	result, started, err := j.systemService.PruneScheduled(ctx, "0", req)
	if started {
		defer func() {
			notifyCtx, cancelNotify := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultHTTPClient)
			defer cancelNotify()
			if sendPruneReportNotificationErr := j.notificationService.SendPruneReportNotification(notifyCtx, result); sendPruneReportNotificationErr != nil {
				slog.WarnContext(ctx, "failed to send prune report notification", "error", sendPruneReportNotificationErr)
			}
		}()
	}
	if err != nil {
		slog.ErrorContext(ctx, "scheduled prune run failed", "error", err)
		return types.SchedulerOutcome{}, err
	}
	if !started {
		slog.InfoContext(ctx, "scheduled prune run skipped; prune already in progress", "activityId", result.ActivityID)
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}

	slog.InfoContext(ctx, "scheduled prune run completed",
		"success", result.Success,
		"spaceReclaimedBytes", result.SpaceReclaimed,
		"containersPruned", len(result.ContainersPruned),
		"imagesDeleted", len(result.ImagesDeleted),
		"volumesDeleted", len(result.VolumesDeleted),
		"networksDeleted", len(result.NetworksDeleted),
		"errors", len(result.Errors),
	)
	if len(result.Errors) > 0 {
		slog.DebugContext(ctx, "scheduled prune run errors", "errors", result.Errors)
	}

	outcome := types.SchedulerOutcome{Status: types.RunStatusSucceeded}
	if result.ActivityID != nil {
		outcome.ActivityID = *result.ActivityID
	}
	if len(result.Errors) > 0 {
		outcome.Status = types.RunStatusPartial
		outcome.Message = "Some resources could not be pruned; see activity details"
	}
	return outcome, nil
}

func buildScheduledPruneRequest(ctx context.Context, settingsService *settings.SettingsService) types.PruneAllRequest {
	return types.PruneAllRequest{
		Containers: buildScheduledPruneOption(ctx, settingsService, "pruneContainerMode", types.PruneContainerModeNone, func(mode types.PruneContainerMode) *types.PruneContainersOptions {
			return &types.PruneContainersOptions{Mode: mode, Until: settingsService.GetStringSetting(ctx, "pruneContainerUntil")}
		}),
		Images: buildScheduledPruneOption(ctx, settingsService, "pruneImageMode", types.PruneImageModeNone, func(mode types.PruneImageMode) *types.PruneImagesOptions {
			return &types.PruneImagesOptions{Mode: mode, Until: settingsService.GetStringSetting(ctx, "pruneImageUntil")}
		}),
		Volumes: buildScheduledPruneOption(ctx, settingsService, "pruneVolumeMode", types.PruneVolumeModeNone, func(mode types.PruneVolumeMode) *types.PruneVolumesOptions {
			return &types.PruneVolumesOptions{Mode: mode}
		}),
		Networks: buildScheduledPruneOption(ctx, settingsService, "pruneNetworkMode", types.PruneNetworkModeNone, func(mode types.PruneNetworkMode) *types.PruneNetworksOptions {
			return &types.PruneNetworksOptions{Mode: mode, Until: settingsService.GetStringSetting(ctx, "pruneNetworkUntil")}
		}),
		BuildCache: buildScheduledPruneOption(ctx, settingsService, "pruneBuildCacheMode", types.PruneBuildCacheModeNone, func(mode types.PruneBuildCacheMode) *types.PruneBuildCacheOptions {
			return &types.PruneBuildCacheOptions{Mode: mode, Until: settingsService.GetStringSetting(ctx, "pruneBuildCacheUntil")}
		}),
	}
}

func buildScheduledPruneOption[M ~string, T any](
	ctx context.Context,
	settingsService *settings.SettingsService,
	modeKey string,
	noneMode M,
	build func(M) *T,
) *T {
	mode := settingsService.GetStringSetting(ctx, modeKey)
	if mode == "" || mode == string(noneMode) {
		return nil
	}
	return build(M(mode))
}

func (j *ScheduledPruneJob) Reconcile(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	return j.systemService.ReconcileScheduledPrune(ctx, previous)
}
