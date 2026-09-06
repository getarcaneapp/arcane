package scheduler

import (
	"context"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imagepatch"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/types/v2"
)

const AutoPatchJobName = "auto-patch"

var autoPatchSystemUser = common.User{
	Username: "System",
}

// AutoPatchJob periodically patches images whose latest vulnerability scan
// found fixable OS package vulnerabilities. It is opt-in via the
// "imageAutoPatchEnabled" setting.
type AutoPatchJob struct {
	imagePatchService *imagepatch.ImagePatchService
	settingsService   *settings.SettingsService
}

// NewAutoPatchJob creates a new AutoPatchJob.
func NewAutoPatchJob(imagePatchService *imagepatch.ImagePatchService, settingsService *settings.SettingsService) *AutoPatchJob {
	return &AutoPatchJob{
		imagePatchService: imagePatchService,
		settingsService:   settingsService,
	}
}

func (j *AutoPatchJob) Name() string {
	return AutoPatchJobName
}

func (j *AutoPatchJob) ShouldSchedule(ctx context.Context) bool {
	return j.settingsService.GetBoolSetting(ctx, "imageAutoPatchEnabled", false)
}

// Schedule returns the cron expression for the job. Defaults to daily at 3 AM.
func (j *AutoPatchJob) Schedule(ctx context.Context) string {
	schedule := j.settingsService.GetStringSetting(ctx, "imageAutoPatchInterval", "0 0 3 * * *")
	if schedule == "" {
		schedule = "0 0 3 * * *"
	}

	parser := scheduleutil.Parser()
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for auto-patch, using default", "invalid_schedule", schedule, "error", err)
		return "0 0 3 * * *"
	}

	return schedule
}

func (j *AutoPatchJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	if !j.settingsService.GetBoolSetting(ctx, "imageAutoPatchEnabled", false) {
		slog.DebugContext(ctx, "scheduled image patching disabled; skipping run")
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	slog.InfoContext(ctx, "scheduled image patching started")

	patched, skipped, err := j.imagePatchService.PatchFlaggedImages(ctx, types.LocalDockerEnvironmentID, autoPatchSystemUser)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled image patching failed", "error", err)
		if patched > 0 {
			return schedulertypes.Outcome{Status: schedulertypes.Partial}, err
		}
		return schedulertypes.Outcome{}, err
	}

	slog.InfoContext(ctx, "scheduled image patching completed",
		"patched", patched,
		"skipped", skipped,
	)
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}
