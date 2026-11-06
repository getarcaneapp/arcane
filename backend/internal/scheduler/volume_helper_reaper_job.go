package scheduler

import (
	"context"
	"log/slog"
	"time"

	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/types/v3"
)

type (
	PruningVolumeHelperJob struct {
		volumeService   *volume.VolumeService
		settingsService *settings.SettingsService
	}
)

const (
	PruningVolumeHelperJobName     = "pruning-volume-helper"
	volumeHelperIdleTimeoutSetting = "volumeHelperIdleTimeout"
	volumeHelperPruningSchedule    = "*/5 * * * *"
)

func NewPruningVolumeHelperJob(volumeService *volume.VolumeService, settingsService *settings.SettingsService) *PruningVolumeHelperJob {
	return &PruningVolumeHelperJob{
		volumeService:   volumeService,
		settingsService: settingsService,
	}
}

func (j *PruningVolumeHelperJob) Name() string {
	return PruningVolumeHelperJobName
}

func (j *PruningVolumeHelperJob) Schedule(ctx context.Context) string {
	return volumeHelperPruningSchedule
}

func (j *PruningVolumeHelperJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	minutes := j.settingsService.GetIntSetting(ctx, volumeHelperIdleTimeoutSetting)
	if minutes <= 0 {
		// 0 (or negative) disables idle pruning.
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}

	removed, err := j.volumeService.ReapIdleHelpers(ctx, time.Duration(minutes)*time.Minute)
	if err != nil {
		slog.ErrorContext(ctx, "volume helper pruning failed", "jobName", PruningVolumeHelperJobName, "error", err)
		return types.SchedulerOutcome{}, err
	}
	if removed > 0 {
		slog.InfoContext(ctx, "volume helper pruning completed",
			"jobName", PruningVolumeHelperJobName,
			"removed", removed,
			"idleTimeoutMinutes", minutes)
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *PruningVolumeHelperJob) Reschedule(ctx context.Context) error {
	return nil
}
