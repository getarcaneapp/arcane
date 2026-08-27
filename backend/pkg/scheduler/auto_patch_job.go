package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imagepatch"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/types/v2"
	"github.com/robfig/cron/v3"
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

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for auto-patch, using default", "invalid_schedule", schedule, "error", err)
		return "0 0 3 * * *"
	}

	return schedule
}

func (j *AutoPatchJob) Run(ctx context.Context) {
	if !j.settingsService.GetBoolSetting(ctx, "imageAutoPatchEnabled", false) {
		slog.DebugContext(ctx, "scheduled image patching disabled; skipping run")
		return
	}

	slog.InfoContext(ctx, "scheduled image patching started")

	patched, skipped, err := j.imagePatchService.PatchFlaggedImages(ctx, types.LocalDockerEnvironmentID, autoPatchSystemUser)
	if err != nil {
		slog.ErrorContext(ctx, "scheduled image patching failed", "error", err)
		return
	}

	slog.InfoContext(ctx, "scheduled image patching completed",
		"patched", patched,
		"skipped", skipped,
	)
}
