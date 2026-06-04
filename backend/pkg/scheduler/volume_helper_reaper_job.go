package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const (
	PruningVolumeHelperJobName = "pruning-volume-helper"

	// volumeHelperIdleTimeoutSetting is the settings key (in minutes) controlling how
	// long a volume-browser helper container may sit idle before it is reaped.
	volumeHelperIdleTimeoutSetting        = "volumeBrowserHelperIdleTimeout"
	defaultVolumeHelperIdleTimeoutMinutes = 10
	volumeHelperPruningSchedule           = "0 */5 * * * *"
)

// PruningVolumeHelperJob periodically removes idle volume-browser helper
// containers. The run frequency is fixed (every 5 minutes); how stale a helper must
// be to be pruned is driven by the volumeBrowserHelperIdleTimeout setting.
type PruningVolumeHelperJob struct {
	volumeService   *services.VolumeService
	settingsService *services.SettingsService
}

func NewPruningVolumeHelperJob(volumeService *services.VolumeService, settingsService *services.SettingsService) *PruningVolumeHelperJob {
	return &PruningVolumeHelperJob{
		volumeService:   volumeService,
		settingsService: settingsService,
	}
}

func (j *PruningVolumeHelperJob) Name() string {
	return PruningVolumeHelperJobName
}

// Schedule runs the pruning job every 5 minutes. This is intentionally not
// configurable; the idle timeout (read in Run) is the user-facing knob.
func (j *PruningVolumeHelperJob) Schedule(ctx context.Context) string {
	return volumeHelperPruningSchedule
}

func (j *PruningVolumeHelperJob) Run(ctx context.Context) {
	if j.volumeService == nil {
		return
	}

	minutes := defaultVolumeHelperIdleTimeoutMinutes
	if j.settingsService != nil {
		minutes = j.settingsService.GetIntSetting(ctx, volumeHelperIdleTimeoutSetting, defaultVolumeHelperIdleTimeoutMinutes)
	}
	if minutes <= 0 {
		// 0 (or negative) disables idle pruning.
		return
	}

	removed, err := j.volumeService.ReapIdleHelpers(ctx, time.Duration(minutes)*time.Minute)
	if err != nil {
		slog.ErrorContext(ctx, "volume helper pruning failed", "jobName", PruningVolumeHelperJobName, "error", err)
		return
	}
	if removed > 0 {
		slog.InfoContext(ctx, "volume helper pruning completed",
			"jobName", PruningVolumeHelperJobName,
			"removed", removed,
			"idleTimeoutMinutes", minutes)
	}
}

func (j *PruningVolumeHelperJob) Reschedule(ctx context.Context) error {
	// Fixed schedule; the idle-timeout setting only affects Run behavior.
	return nil
}
