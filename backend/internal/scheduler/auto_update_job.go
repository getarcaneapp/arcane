package scheduler

import (
	"context"

	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/schedule"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/updater"
)

const (
	autoUpdateDefaultSchedule = "0 0 * * *"
)

// NewAutoUpdateJob runs the durable auto-update workflow on the configured schedule.
// Interrupted runs that cannot resume fall back to frozen-plan recovery.
func NewAutoUpdateJob(engine *flow.Engine, updaterService *updater.UpdaterService, settingsService *settings.SettingsService) *flow.Job {
	return &flow.Job{
		Engine:   engine,
		Workflow: updaterService.AutoUpdateWorkflow(),
		JobName:  "auto-update",
		ScheduleFn: func(ctx context.Context) string {
			return schedule.Or(ctx, settingsService.GetStringSetting(ctx, "autoUpdateInterval"), autoUpdateDefaultSchedule, "auto-update")
		},
		ShouldRunFn: func(ctx context.Context) bool {
			return settingsService.GetBoolSetting(ctx, "autoUpdate") && settingsService.GetBoolSetting(ctx, "pollingEnabled")
		},
		FallbackFn:      updaterService.ReconcilePending,
		ValidateRetryFn: updaterService.ValidateAutoUpdateRetry,
	}
}
