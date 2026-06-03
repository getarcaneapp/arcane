package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const VolumeHelperReaperJobName = "volume-helper-reaper"

// volumeHelperIdleTimeoutSetting is the settings key (in minutes) controlling how
// long a volume-browser helper container may sit idle before it is reaped.
const volumeHelperIdleTimeoutSetting = "volumeBrowserHelperIdleTimeout"

const defaultVolumeHelperIdleTimeoutMinutes = 5

const volumeHelperReaperSchedule = "0 */5 * * * *"

// VolumeHelperReaperJob periodically removes idle volume-browser helper
// containers. The run frequency is fixed (every 5 minutes); how stale a helper must
// be to be reaped is driven by the volumeBrowserHelperIdleTimeout setting.
type VolumeHelperReaperJob struct {
	volumeService   *services.VolumeService
	settingsService *services.SettingsService
}

func NewVolumeHelperReaperJob(volumeService *services.VolumeService, settingsService *services.SettingsService) *VolumeHelperReaperJob {
	return &VolumeHelperReaperJob{
		volumeService:   volumeService,
		settingsService: settingsService,
	}
}

func (j *VolumeHelperReaperJob) Name() string {
	return VolumeHelperReaperJobName
}

// Schedule runs the reaper every 5 minutes. This is intentionally not
// configurable; the idle timeout (read in Run) is the user-facing knob.
func (j *VolumeHelperReaperJob) Schedule(ctx context.Context) string {
	return volumeHelperReaperSchedule
}

func (j *VolumeHelperReaperJob) Run(ctx context.Context) {
	if j.volumeService == nil {
		return
	}

	minutes := defaultVolumeHelperIdleTimeoutMinutes
	if j.settingsService != nil {
		minutes = j.settingsService.GetIntSetting(ctx, volumeHelperIdleTimeoutSetting, defaultVolumeHelperIdleTimeoutMinutes)
	}
	if minutes <= 0 {
		// 0 (or negative) disables idle reaping.
		return
	}

	removed, err := j.volumeService.ReapIdleHelpers(ctx, time.Duration(minutes)*time.Minute)
	if err != nil {
		slog.ErrorContext(ctx, "volume helper reaper failed", "jobName", VolumeHelperReaperJobName, "error", err)
		return
	}
	if removed > 0 {
		slog.InfoContext(ctx, "volume helper reaper completed",
			"jobName", VolumeHelperReaperJobName,
			"removed", removed,
			"idleTimeoutMinutes", minutes)
	}
}

func (j *VolumeHelperReaperJob) Reschedule(ctx context.Context) error {
	// Fixed schedule; the idle-timeout setting only affects Run behavior.
	return nil
}
