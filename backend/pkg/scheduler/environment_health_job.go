package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

type EnvironmentHealthJob struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
}

func NewEnvironmentHealthJob(environmentService *services.EnvironmentService, settingsService *services.SettingsService) *EnvironmentHealthJob {
	return &EnvironmentHealthJob{
		environmentService: environmentService,
		settingsService:    settingsService,
	}
}

func (j *EnvironmentHealthJob) Name() string {
	return "environment-health"
}

func (j *EnvironmentHealthJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "environmentHealthInterval", "0 */2 * * * *")
	if s == "" {
		return "0 */2 * * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(s); err == nil {
		if i <= 0 {
			i = 2
		}
		if i%60 == 0 {
			return fmt.Sprintf("0 0 */%d * * *", i/60)
		}
		return fmt.Sprintf("0 */%d * * * *", i)
	}

	return s
}

func (j *EnvironmentHealthJob) Run(ctx context.Context) {
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
		return
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
}

func (j *EnvironmentHealthJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling environment health job in new scheduler; currently requires restart")
	return nil
}
