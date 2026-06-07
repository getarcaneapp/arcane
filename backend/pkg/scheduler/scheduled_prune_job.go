package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/system"
	"github.com/robfig/cron/v3"
)

const ScheduledPruneJobName = "scheduled-prune"

type ScheduledPruneJob struct {
	systemService       *services.SystemService
	settingsService     *services.SettingsService
	notificationService *services.NotificationService
}

func NewScheduledPruneJob(systemService *services.SystemService, settingsService *services.SettingsService, notificationService *services.NotificationService) *ScheduledPruneJob {
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
	schedule := j.settingsService.GetStringSetting(ctx, "scheduledPruneInterval", "0 0 0 * * *")
	if schedule == "" {
		schedule = "0 0 0 * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(schedule); err == nil {
		if i <= 0 {
			i = 1440
		}
		switch {
		case i%1440 == 0:
			schedule = fmt.Sprintf("0 0 0 */%d * *", i/1440)
		case i%60 == 0:
			schedule = fmt.Sprintf("0 0 */%d * * *", i/60)
		default:
			schedule = fmt.Sprintf("0 */%d * * * *", i)
		}
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for scheduled-prune, using default", "invalid_schedule", schedule, "error", err)
		return "0 0 0 * * *"
	}

	return schedule
}

func (j *ScheduledPruneJob) Run(ctx context.Context) {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	if !enabled {
		slog.DebugContext(ctx, "scheduled prune disabled; skipping run")
		return
	}

	req := buildScheduledPruneRequestInternal(ctx, j.settingsService)

	if !hasScheduledPruneTargetsInternal(req) {
		slog.InfoContext(ctx, "scheduled prune run skipped; no resource types selected")
		return
	}

	slog.InfoContext(ctx, "scheduled prune run started",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"build_cache", req.BuildCache,
	)

	result, err := j.systemService.PruneAll(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled prune run failed", "error", err)
		return
	}

	slog.InfoContext(ctx, "scheduled prune run completed",
		"success", result.Success,
		"space_reclaimed_bytes", result.SpaceReclaimed,
		"containers_pruned", len(result.ContainersPruned),
		"images_deleted", len(result.ImagesDeleted),
		"volumes_deleted", len(result.VolumesDeleted),
		"networks_deleted", len(result.NetworksDeleted),
		"errors", len(result.Errors),
	)
	if len(result.Errors) > 0 {
		slog.DebugContext(ctx, "scheduled prune run errors", "errors", result.Errors)
	}

	// Send notification
	if err := j.notificationService.SendPruneReportNotification(ctx, result); err != nil {
		slog.WarnContext(ctx, "failed to send prune report notification", "error", err)
	}
}

func (j *ScheduledPruneJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling scheduled prune job in new scheduler; currently requires restart")
	return nil
}

func buildScheduledPruneRequestInternal(ctx context.Context, settingsService *services.SettingsService) system.PruneAllRequest {
	return system.PruneAllRequest{
		Containers: buildScheduledContainerPruneOptionsInternal(ctx, settingsService),
		Images:     buildScheduledImagePruneOptionsInternal(ctx, settingsService),
		Volumes:    buildScheduledVolumePruneOptionsInternal(ctx, settingsService),
		Networks:   buildScheduledNetworkPruneOptionsInternal(ctx, settingsService),
		BuildCache: buildScheduledBuildCachePruneOptionsInternal(ctx, settingsService),
	}
}

func hasScheduledPruneTargetsInternal(req system.PruneAllRequest) bool {
	return req.Containers != nil || req.Images != nil || req.Volumes != nil || req.Networks != nil || req.BuildCache != nil
}

func buildScheduledContainerPruneOptionsInternal(ctx context.Context, settingsService *services.SettingsService) *system.PruneContainersOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneContainerMode", "stopped")
	if mode == "" || mode == string(system.PruneContainerModeNone) {
		return nil
	}

	return &system.PruneContainersOptions{
		Mode:  system.PruneContainerMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneContainerUntil", ""),
	}
}

func buildScheduledImagePruneOptionsInternal(ctx context.Context, settingsService *services.SettingsService) *system.PruneImagesOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneImageMode", "dangling")
	if mode == "" || mode == string(system.PruneImageModeNone) {
		return nil
	}

	return &system.PruneImagesOptions{
		Mode:  system.PruneImageMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneImageUntil", ""),
	}
}

func buildScheduledVolumePruneOptionsInternal(ctx context.Context, settingsService *services.SettingsService) *system.PruneVolumesOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneVolumeMode", "none")
	if mode == "" || mode == string(system.PruneVolumeModeNone) {
		return nil
	}

	return &system.PruneVolumesOptions{Mode: system.PruneVolumeMode(mode)}
}

func buildScheduledNetworkPruneOptionsInternal(ctx context.Context, settingsService *services.SettingsService) *system.PruneNetworksOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneNetworkMode", "unused")
	if mode == "" || mode == string(system.PruneNetworkModeNone) {
		return nil
	}

	return &system.PruneNetworksOptions{
		Mode:  system.PruneNetworkMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneNetworkUntil", ""),
	}
}

func buildScheduledBuildCachePruneOptionsInternal(ctx context.Context, settingsService *services.SettingsService) *system.PruneBuildCacheOptions {
	mode := settingsService.GetStringSetting(ctx, "pruneBuildCacheMode", "none")
	if mode == "" || mode == string(system.PruneBuildCacheModeNone) {
		return nil
	}

	return &system.PruneBuildCacheOptions{
		Mode:  system.PruneBuildCacheMode(mode),
		Until: settingsService.GetStringSetting(ctx, "pruneBuildCacheUntil", ""),
	}
}
