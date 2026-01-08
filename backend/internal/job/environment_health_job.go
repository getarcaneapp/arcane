package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/go-co-op/gocron/v2"
)

type EnvironmentHealthJob struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	scheduler          *Scheduler
}

func NewEnvironmentHealthJob(scheduler *Scheduler, environmentService *services.EnvironmentService, settingsService *services.SettingsService) *EnvironmentHealthJob {
	return &EnvironmentHealthJob{
		environmentService: environmentService,
		settingsService:    settingsService,
		scheduler:          scheduler,
	}
}

func (j *EnvironmentHealthJob) Register(ctx context.Context) error {
	healthCheckInterval := j.settingsService.GetIntSetting(ctx, "environmentHealthInterval", 2)
	interval := time.Duration(healthCheckInterval) * time.Minute

	// Ensure minimum interval of 1 minute
	if interval < 1*time.Minute {
		slog.WarnContext(ctx, "environment health check interval too low; using minimum", "requested_minutes", healthCheckInterval, "effective_interval", "1m")
		interval = 1 * time.Minute
	}

	slog.InfoContext(ctx, "registering environment health check job", "interval", interval.String())

	j.scheduler.RemoveJobByName("environment-health")

	jobDefinition := gocron.DurationJob(interval)
	return j.scheduler.RegisterJob(
		ctx,
		"environment-health",
		jobDefinition,
		j.Execute,
		true, // Run immediately on startup
	)
}

func (j *EnvironmentHealthJob) Reschedule(ctx context.Context) error {
	healthCheckInterval := j.settingsService.GetIntSetting(ctx, "environmentHealthInterval", 2)
	interval := time.Duration(healthCheckInterval) * time.Minute

	if interval < 1*time.Minute {
		interval = 1 * time.Minute
	}

	slog.InfoContext(ctx, "environment health check settings changed; rescheduling", "interval", interval.String())

	return j.scheduler.RescheduleDurationJobByName(ctx, "environment-health", interval, j.Execute, false)
}

func (j *EnvironmentHealthJob) Execute(ctx context.Context) error {
	slog.InfoContext(ctx, "environment health check started")

	// Get all environments using the DB directly
	db := j.environmentService.GetDB()
	var environments []struct {
		ID      string
		Name    string
		Enabled bool
	}

	if err := db.WithContext(ctx).
		Model(&struct {
			ID      string `gorm:"column:id"`
			Name    string `gorm:"column:name"`
			Enabled bool   `gorm:"column:enabled"`
		}{}).
		Table("environments").
		Where("enabled = ?", true).
		Find(&environments).Error; err != nil {
		slog.ErrorContext(ctx, "failed to list environments for health check", "error", err)
		return err
	}

	checkedCount := 0
	onlineCount := 0
	offlineCount := 0

	for _, env := range environments {
		checkedCount++

		// Test connection without custom URL (will update DB status)
		status, err := j.environmentService.TestConnection(ctx, env.ID, nil)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "environment health check failed", "environment_id", env.ID, "environment_name", env.Name, "status", status, "error", err)
			offlineCount++
		case status == "online":
			onlineCount++
			// Sync registries and git repositories to online remote environments (skip local environment ID "0")
			if env.ID != "0" {
				go func(envID, envName string) {
					syncCtx := context.WithoutCancel(ctx)
					if err := j.environmentService.SyncRegistriesToEnvironment(syncCtx, envID); err != nil {
						slog.WarnContext(syncCtx, "failed to sync registries during health check",
							"environment_id", envID,
							"environment_name", envName,
							"error", err)
					} else {
						slog.DebugContext(syncCtx, "successfully synced registries during health check",
							"environment_id", envID,
							"environment_name", envName)
					}
				}(env.ID, env.Name)
				go func(envID, envName string) {
					syncCtx := context.WithoutCancel(ctx)
					if err := j.environmentService.SyncRepositoriesToEnvironment(syncCtx, envID); err != nil {
						slog.WarnContext(syncCtx, "failed to sync git repositories during health check",
							"environment_id", envID,
							"environment_name", envName,
							"error", err)
					} else {
						slog.DebugContext(syncCtx, "successfully synced git repositories during health check",
							"environment_id", envID,
							"environment_name", envName)
					}
				}(env.ID, env.Name)
			}
		default:
			offlineCount++
		}
	}

	slog.InfoContext(ctx, "environment health check completed", "checked", checkedCount, "online", onlineCount, "offline", offlineCount)

	return nil
}
