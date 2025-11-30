package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/go-co-op/gocron/v2"
)

type AutoUpdateJob struct {
	updaterService  *services.UpdaterService
	settingsService *services.SettingsService
	scheduler       *Scheduler
}

func NewAutoUpdateJob(scheduler *Scheduler, updaterService *services.UpdaterService, settingsService *services.SettingsService) *AutoUpdateJob {
	return &AutoUpdateJob{
		updaterService:  updaterService,
		settingsService: settingsService,
		scheduler:       scheduler,
	}
}

func (j *AutoUpdateJob) Register(ctx context.Context) error {
	autoUpdateEnabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	autoUpdateInterval := j.settingsService.GetIntSetting(ctx, "autoUpdateInterval", 1440)

	if !autoUpdateEnabled || !pollingEnabled {
		slog.InfoContext(ctx, "auto-update disabled or polling disabled; job not registered",
			"autoUpdate", autoUpdateEnabled, "pollingEnabled", pollingEnabled)
		return nil
	}

	interval := time.Duration(autoUpdateInterval) * time.Minute
	if interval < 5*time.Minute {
		slog.WarnContext(ctx, "auto-update interval too low; using default",
			"requested_minutes", autoUpdateInterval,
			"effective_interval", "60m")
		interval = 60 * time.Minute
	}

	slog.InfoContext(ctx, "registering auto-update job", "interval", interval.String())

	// ensure single instance
	j.scheduler.RemoveJobByName("auto-update")

	jobDefinition := gocron.DurationJob(interval)
	return j.scheduler.RegisterJob(
		ctx,
		"auto-update",
		jobDefinition,
		j.Execute,
		false,
	)
}

func (j *AutoUpdateJob) Execute(ctx context.Context) error {
	slog.InfoContext(ctx, "auto-update run started")

	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !enabled || !pollingEnabled {
		slog.InfoContext(ctx, "auto-update disabled or polling disabled; skipping run",
			"autoUpdate", enabled, "pollingEnabled", pollingEnabled)
		return nil
	}

	result, err := j.updaterService.ApplyPending(ctx, false)
	if err != nil {
		slog.ErrorContext(ctx, "auto-update run failed", "err", err)
		return err
	}

	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)

	return nil
}

func (j *AutoUpdateJob) Reschedule(ctx context.Context) error {
	autoUpdateEnabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	autoUpdateInterval := j.settingsService.GetIntSetting(ctx, "autoUpdateInterval", 1440)

	if !autoUpdateEnabled || !pollingEnabled {
		j.scheduler.RemoveJobByName("auto-update")
		slog.InfoContext(ctx, "auto-update disabled or polling disabled; removed job if present",
			"autoUpdate", autoUpdateEnabled, "pollingEnabled", pollingEnabled)
		return nil
	}

	interval := time.Duration(autoUpdateInterval) * time.Minute
	if interval < 5*time.Minute {
		interval = 60 * time.Minute
	}
	slog.InfoContext(ctx, "auto-update settings changed; rescheduling", "interval", interval.String())

	return j.scheduler.RescheduleDurationJobByName(ctx, "auto-update", interval, j.Execute, false)
}
