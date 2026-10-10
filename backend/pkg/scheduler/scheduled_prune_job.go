package scheduler

import (
	"context"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"

	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
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

func (j *ScheduledPruneJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	if !enabled {
		slog.DebugContext(ctx, "scheduled prune disabled; skipping run")
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	req := buildScheduledPruneRequestInternal(ctx, j.settingsService)

	if !hasScheduledPruneTargetsInternal(req) {
		slog.InfoContext(ctx, "scheduled prune run skipped; no resource types selected")
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	slog.InfoContext(ctx, "scheduled prune run started",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"buildCache", req.BuildCache,
	)

	result, started, err := j.systemService.PruneScheduled(ctx, "0", req)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled prune run failed", "error", err)
		return schedulertypes.Outcome{}, err
	}
	if !started {
		slog.InfoContext(ctx, "scheduled prune run skipped; prune already in progress", "activityId", result.ActivityID)
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
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

	// Send notification
	if sendPruneReportNotificationErr := j.notificationService.SendPruneReportNotification(ctx, result); sendPruneReportNotificationErr != nil {
		slog.WarnContext(ctx, "failed to send prune report notification", "error", sendPruneReportNotificationErr)
	}
	outcome := schedulertypes.Outcome{Status: schedulertypes.Succeeded}
	if result.ActivityID != nil {
		outcome.ActivityID = *result.ActivityID
	}
	if len(result.Errors) > 0 {
		outcome.Status = schedulertypes.Partial
		outcome.Message = "Some resources could not be pruned; see activity details"
	}
	return outcome, nil
}

func (j *ScheduledPruneJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling scheduled prune job in new scheduler; currently requires restart")
	return nil
}

func buildScheduledPruneRequestInternal(ctx context.Context, settingsService *settings.SettingsService) systemtypes.PruneAllRequest {
	return systemtypes.PruneAllRequest{
		Containers: buildScheduledContainerPruneOptionsInternal(ctx, settingsService),
		Images:     buildScheduledImagePruneOptionsInternal(ctx, settingsService),
		Volumes:    buildScheduledVolumePruneOptionsInternal(ctx, settingsService),
		Networks:   buildScheduledNetworkPruneOptionsInternal(ctx, settingsService),
		BuildCache: buildScheduledBuildCachePruneOptionsInternal(ctx, settingsService),
	}
}

func hasScheduledPruneTargetsInternal(req systemtypes.PruneAllRequest) bool {
	return req.Containers != nil || req.Images != nil || req.Volumes != nil || req.Networks != nil || req.BuildCache != nil
}

func buildScheduledContainerPruneOptionsInternal(ctx context.Context, settingsService *settings.SettingsService) *systemtypes.PruneContainersOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneContainerMode", "stopped")
	if mode == "" || mode == string(systemtypes.PruneContainerModeNone) {
		return nil
	}

	return &systemtypes.PruneContainersOptions{
		Mode:  systemtypes.PruneContainerMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneContainerUntil", ""),
	}
}

func buildScheduledImagePruneOptionsInternal(ctx context.Context, settingsService *settings.SettingsService) *systemtypes.PruneImagesOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneImageMode", "dangling")
	if mode == "" || mode == string(systemtypes.PruneImageModeNone) {
		return nil
	}

	return &systemtypes.PruneImagesOptions{
		Mode:  systemtypes.PruneImageMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneImageUntil", ""),
	}
}

func buildScheduledVolumePruneOptionsInternal(ctx context.Context, settingsService *settings.SettingsService) *systemtypes.PruneVolumesOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneVolumeMode", "none")
	if mode == "" || mode == string(systemtypes.PruneVolumeModeNone) {
		return nil
	}
	return &systemtypes.PruneVolumesOptions{Mode: systemtypes.PruneVolumeMode(mode)}
}

func buildScheduledNetworkPruneOptionsInternal(ctx context.Context, settingsService *settings.SettingsService) *systemtypes.PruneNetworksOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneNetworkMode", "unused")
	if mode == "" || mode == string(systemtypes.PruneNetworkModeNone) {
		return nil
	}

	return &systemtypes.PruneNetworksOptions{
		Mode:  systemtypes.PruneNetworkMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneNetworkUntil", ""),
	}
}

func buildScheduledBuildCachePruneOptionsInternal(ctx context.Context, settingsService *settings.SettingsService) *systemtypes.PruneBuildCacheOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneBuildCacheMode", "none")
	if mode == "" || mode == string(systemtypes.PruneBuildCacheModeNone) {
		return nil
	}

	return &systemtypes.PruneBuildCacheOptions{
		Mode:  systemtypes.PruneBuildCacheMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneBuildCacheUntil", ""),
	}
}

func (j *ScheduledPruneJob) Reconcile(ctx context.Context, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
	return j.systemService.ReconcileScheduledPrune(ctx, previous)
}
