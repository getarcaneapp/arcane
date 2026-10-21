package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
)

const ScheduledPruneJobName = "scheduled-prune"

type ScheduledPruneJob struct {
	systemService       *system.SystemService
	settingsService     *settings.SettingsService
	notificationService *notification.NotificationService
}

func NewScheduledPruneJob(systemService *system.SystemService, settingsService *settings.SettingsService, notificationService *notification.NotificationService) *ScheduledPruneJob {
	return &ScheduledPruneJob{
		systemService:       systemService,
		settingsService:     settingsService,
		notificationService: notificationService,
	}
}

func (j *ScheduledPruneJob) Name() string {
	return ScheduledPruneJobName
}

func (j *ScheduledPruneJob) ShouldSchedule(ctx context.Context) bool {
	return j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
}

func (j *ScheduledPruneJob) Schedule(ctx context.Context) string {
	return scheduleutil.Or(ctx, j.settingsService.GetStringSetting(ctx, "scheduledPruneInterval", "0 0 0 * * *"), "0 0 0 * * *", "scheduled-prune")
}

func (j *ScheduledPruneJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	if !enabled {
		slog.DebugContext(ctx, "scheduled prune disabled; skipping run")
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}

	req := buildScheduledPruneRequest(ctx, j.settingsService)

	if !hasScheduledPruneTargets(req) {
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

func (j *ScheduledPruneJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling scheduled prune job in new scheduler; currently requires restart")
	return nil
}

func buildScheduledPruneRequest(ctx context.Context, settingsService *settings.SettingsService) types.PruneAllRequest {
	return types.PruneAllRequest{
		Containers: buildScheduledContainerPruneOptions(ctx, settingsService),
		Images:     buildScheduledImagePruneOptions(ctx, settingsService),
		Volumes:    buildScheduledVolumePruneOptions(ctx, settingsService),
		Networks:   buildScheduledNetworkPruneOptions(ctx, settingsService),
		BuildCache: buildScheduledBuildCachePruneOptions(ctx, settingsService),
	}
}

func hasScheduledPruneTargets(req types.PruneAllRequest) bool {
	return req.Containers != nil || req.Images != nil || req.Volumes != nil || req.Networks != nil || req.BuildCache != nil
}

func buildScheduledContainerPruneOptions(ctx context.Context, settingsService *settings.SettingsService) *types.PruneContainersOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneContainerMode", "stopped")
	if mode == "" || mode == string(types.PruneContainerModeNone) {
		return nil
	}

	return &types.PruneContainersOptions{
		Mode:  types.PruneContainerMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneContainerUntil", ""),
	}
}

func buildScheduledImagePruneOptions(ctx context.Context, settingsService *settings.SettingsService) *types.PruneImagesOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneImageMode", "dangling")
	if mode == "" || mode == string(types.PruneImageModeNone) {
		return nil
	}

	return &types.PruneImagesOptions{
		Mode:  types.PruneImageMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneImageUntil", ""),
	}
}

func buildScheduledVolumePruneOptions(ctx context.Context, settingsService *settings.SettingsService) *types.PruneVolumesOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneVolumeMode", "none")
	if mode == "" || mode == string(types.PruneVolumeModeNone) {
		return nil
	}
	return &types.PruneVolumesOptions{Mode: types.PruneVolumeMode(mode)}
}

func buildScheduledNetworkPruneOptions(ctx context.Context, settingsService *settings.SettingsService) *types.PruneNetworksOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneNetworkMode", "unused")
	if mode == "" || mode == string(types.PruneNetworkModeNone) {
		return nil
	}

	return &types.PruneNetworksOptions{
		Mode:  types.PruneNetworkMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneNetworkUntil", ""),
	}
}

func buildScheduledBuildCachePruneOptions(ctx context.Context, settingsService *settings.SettingsService) *types.PruneBuildCacheOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneBuildCacheMode", "none")
	if mode == "" || mode == string(types.PruneBuildCacheModeNone) {
		return nil
	}

	return &types.PruneBuildCacheOptions{
		Mode:  types.PruneBuildCacheMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneBuildCacheUntil", ""),
	}
}

func (j *ScheduledPruneJob) Reconcile(ctx context.Context, previous types.SchedulerRun) (types.SchedulerOutcome, error) {
	return j.systemService.ReconcileScheduledPrune(ctx, previous)
}
