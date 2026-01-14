package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/system"
	"github.com/go-co-op/gocron/v2"
)

const ScheduledPruneJobName = "scheduled-prune"

type ScheduledPruneJob struct {
	systemService   *services.SystemService
	settingsService *services.SettingsService
	scheduler       *Scheduler
}

func NewScheduledPruneJob(scheduler *Scheduler, systemService *services.SystemService, settingsService *services.SettingsService) *ScheduledPruneJob {
	return &ScheduledPruneJob{
		systemService:   systemService,
		settingsService: settingsService,
		scheduler:       scheduler,
	}
}

func (j *ScheduledPruneJob) Register(ctx context.Context) error {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	intervalMinutes := j.settingsService.GetIntSetting(ctx, "scheduledPruneInterval", 1440)

	if !enabled {
		j.scheduler.RemoveJobByName(ScheduledPruneJobName)
		slog.InfoContext(ctx, "scheduled prune disabled; job not registered", "enabled", enabled)
		return nil
	}

	interval := time.Duration(intervalMinutes) * time.Minute
	if interval < 60*time.Minute {
		slog.WarnContext(ctx, "scheduled prune interval too low; using minimum", "requested_minutes", intervalMinutes, "effective_interval", "60m")
		interval = 60 * time.Minute
	}

	j.scheduler.RemoveJobByName(ScheduledPruneJobName)

	jobDefinition := gocron.DurationJob(interval)
	return j.scheduler.RegisterJob(
		ctx,
		ScheduledPruneJobName,
		jobDefinition,
		j.Execute,
		false,
	)
}

func (j *ScheduledPruneJob) Execute(ctx context.Context) error {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	if !enabled {
		slog.InfoContext(ctx, "scheduled prune disabled; skipping run")
		return nil
	}

	pruneMode := j.settingsService.GetStringSetting(ctx, "dockerPruneMode", "dangling")
	danglingOnly := pruneMode != "all"

	req := system.PruneAllRequest{
		Containers: j.settingsService.GetBoolSetting(ctx, "scheduledPruneContainers", true),
		Images:     j.settingsService.GetBoolSetting(ctx, "scheduledPruneImages", true),
		Volumes:    j.settingsService.GetBoolSetting(ctx, "scheduledPruneVolumes", false),
		Networks:   j.settingsService.GetBoolSetting(ctx, "scheduledPruneNetworks", true),
		BuildCache: j.settingsService.GetBoolSetting(ctx, "scheduledPruneBuildCache", false),
		Dangling:   danglingOnly,
	}

	if !req.Containers && !req.Images && !req.Volumes && !req.Networks && !req.BuildCache {
		slog.InfoContext(ctx, "scheduled prune run skipped; no resource types selected")
		return nil
	}

	slog.InfoContext(ctx, "scheduled prune run started",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"build_cache", req.BuildCache,
		"dangling_only", req.Dangling,
	)

	result, err := j.systemService.PruneAll(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled prune run failed", "error", err)
		return err
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

	return nil
}

func (j *ScheduledPruneJob) Reschedule(ctx context.Context) error {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	intervalMinutes := j.settingsService.GetIntSetting(ctx, "scheduledPruneInterval", 1440)

	if !enabled {
		j.scheduler.RemoveJobByName(ScheduledPruneJobName)
		slog.InfoContext(ctx, "scheduled prune disabled; removed job if present")
		return nil
	}

	interval := time.Duration(intervalMinutes) * time.Minute
	if interval < 60*time.Minute {
		interval = 60 * time.Minute
	}

	slog.InfoContext(ctx, "scheduled prune settings changed; rescheduling", "interval", interval.String())

	return j.scheduler.RescheduleDurationJobByName(ctx, ScheduledPruneJobName, interval, j.Execute, false)
}
