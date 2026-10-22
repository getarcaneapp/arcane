package scheduler

import (
	"context"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/schedule"
)

const (
	AutoPatchJobName     = "auto-patch"
	autoPatchDefaultCron = "0 0 3 * * *"
)

// NewAutoPatchJob runs the durable auto-patch workflow. It is opt-in via the
// "imageAutoPatchEnabled" setting and defaults to daily at 03:00.
func NewAutoPatchJob(engine *flow.Engine, imageService *image.ImageService, settingsService *settings.SettingsService) *flow.Job {
	return &flow.Job{
		Engine:   engine,
		Workflow: imageService.PatchWorkflow(),
		JobName:  AutoPatchJobName,
		ScheduleFn: func(ctx context.Context) string {
			return scheduleutil.Or(ctx, settingsService.GetStringSetting(ctx, "imageAutoPatchInterval"), autoPatchDefaultCron, AutoPatchJobName)
		},
		ShouldRunFn: func(ctx context.Context) bool {
			return settingsService.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement) && settingsService.GetBoolSetting(ctx, "imageAutoPatchEnabled")
		},
	}
}
