package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/system"
)

const ScheduledPruneJobName = "scheduled-prune"

type ScheduledPruneJob struct {
	systemService   *services.SystemService
	settingsService *services.SettingsService
}

func NewScheduledPruneJob(systemService *services.SystemService, settingsService *services.SettingsService) *ScheduledPruneJob {
	return &ScheduledPruneJob{
		systemService:   systemService,
		settingsService: settingsService,
	}
}

func (j *ScheduledPruneJob) Name() string {
	return ScheduledPruneJobName
}

func (j *ScheduledPruneJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "scheduledPruneInterval", "0 0 0 * * *")
	if s == "" {
		return "0 0 0 * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(s); err == nil {
		if i <= 0 {
			i = 1440
		}
		if i%1440 == 0 {
			return fmt.Sprintf("0 0 0 */%d * *", i/1440)
		}
		if i%60 == 0 {
			return fmt.Sprintf("0 0 */%d * * *", i/60)
		}
		return fmt.Sprintf("0 */%d * * * *", i)
	}

	return s
}

func (j *ScheduledPruneJob) Run(ctx context.Context) {
	enabled := j.settingsService.GetBoolSetting(ctx, "scheduledPruneEnabled", false)
	if !enabled {
		slog.DebugContext(ctx, "scheduled prune disabled; skipping run")
		return
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
		return
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
}

func (j *ScheduledPruneJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling scheduled prune job in new scheduler; currently requires restart")
	return nil
}
