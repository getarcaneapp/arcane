package scheduler

import (
	"context"

	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
)

const autoUpdateDefaultSchedule = "0 0 0 * * *"

// NewAutoUpdateJob runs the durable auto-update workflow on the configured schedule.
// Interrupted runs that cannot resume fall back to frozen-plan recovery.
func NewAutoUpdateJob(engine *flow.Engine, updaterService *updater.UpdaterService, settingsService *settings.SettingsService) *flow.Job {
	return &flow.Job{
		Engine:   engine,
		Workflow: updaterService.AutoUpdateWorkflow(),
		JobName:  "auto-update",
		ScheduleFn: func(ctx context.Context) string {
			schedule := settingsService.GetStringSetting(ctx, "autoUpdateInterval", autoUpdateDefaultSchedule)
			return kit.Ternary(schedule == "", autoUpdateDefaultSchedule, schedule)
		},
		ShouldRunFn: func(ctx context.Context) bool {
			return settingsService.GetBoolSetting(ctx, "autoUpdate", false) && settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
		},
		FallbackFn:      updaterService.ReconcilePending,
		ValidateRetryFn: updaterService.ValidateAutoUpdateRetry,
	}
}
