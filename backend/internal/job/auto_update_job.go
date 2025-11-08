package job

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/ofkm/arcane-backend/internal/services"
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

	if !autoUpdateEnabled || !pollingEnabled {
		slog.InfoContext(ctx, "auto-update disabled or polling disabled; job not registered",
			"autoUpdate", autoUpdateEnabled, "pollingEnabled", pollingEnabled)
		return nil
	}

	autoUpdateCron := j.settingsService.GetStringSetting(ctx, "autoUpdateCron", "")
	autoUpdateCron = strings.TrimSpace(autoUpdateCron)

	// ensure single instance
	j.scheduler.RemoveJobByName("auto-update")

	var jobDefinition gocron.JobDefinition
	var scheduleDesc string

	if autoUpdateCron == "" {
		// Immediate mode: check for updates frequently (every 5 minutes)
		jobDefinition = gocron.DurationJob(5 * time.Minute)
		scheduleDesc = "immediate (every 5 minutes)"
	} else {
		// Cron-based scheduling
		// Note: gocron.CronJob may panic on invalid cron, but we'll let the scheduler handle it
		jobDefinition = gocron.CronJob(autoUpdateCron, false)
		scheduleDesc = autoUpdateCron
	}

	slog.InfoContext(ctx, "registering auto-update job", "schedule", scheduleDesc)

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

	if !autoUpdateEnabled || !pollingEnabled {
		j.scheduler.RemoveJobByName("auto-update")
		slog.InfoContext(ctx, "auto-update disabled or polling disabled; removed job if present",
			"autoUpdate", autoUpdateEnabled, "pollingEnabled", pollingEnabled)
		return nil
	}

	autoUpdateCron := j.settingsService.GetStringSetting(ctx, "autoUpdateCron", "")
	autoUpdateCron = strings.TrimSpace(autoUpdateCron)

	// Remove existing job
	j.scheduler.RemoveJobByName("auto-update")

	var jobDefinition gocron.JobDefinition
	var scheduleDesc string

	if autoUpdateCron == "" {
		// Immediate mode: check for updates frequently (every 5 minutes)
		jobDefinition = gocron.DurationJob(5 * time.Minute)
		scheduleDesc = "immediate (every 5 minutes)"
	} else {
		// Cron-based scheduling
		// Note: gocron.CronJob may panic on invalid cron, but we'll let the scheduler handle it
		jobDefinition = gocron.CronJob(autoUpdateCron, false)
		scheduleDesc = autoUpdateCron
	}

	slog.InfoContext(ctx, "auto-update settings changed; rescheduling", "schedule", scheduleDesc)

	return j.scheduler.RegisterJob(
		ctx,
		"auto-update",
		jobDefinition,
		j.Execute,
		false,
	)
}
