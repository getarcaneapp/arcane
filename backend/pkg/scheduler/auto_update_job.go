package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

type AutoUpdateJob struct {
	updaterService  *services.UpdaterService
	settingsService *services.SettingsService
}

func NewAutoUpdateJob(updaterService *services.UpdaterService, settingsService *services.SettingsService) *AutoUpdateJob {
	return &AutoUpdateJob{
		updaterService:  updaterService,
		settingsService: settingsService,
	}
}

func (j *AutoUpdateJob) Name() string {
	return "auto-update"
}

func (j *AutoUpdateJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "autoUpdateInterval", "0 0 0 * * *")
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

func (j *AutoUpdateJob) Run(ctx context.Context) {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !enabled || !pollingEnabled {
		slog.DebugContext(ctx, "auto-update disabled or polling disabled; skipping run",
			"autoUpdate", enabled, "pollingEnabled", pollingEnabled)
		return
	}

	slog.InfoContext(ctx, "auto-update run started")

	result, err := j.updaterService.ApplyPending(ctx, false)
	if err != nil {
		slog.ErrorContext(ctx, "auto-update run failed", "err", err)
		return
	}

	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
}

func (j *AutoUpdateJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling auto-update job in new scheduler; currently requires restart")
	return nil
}
