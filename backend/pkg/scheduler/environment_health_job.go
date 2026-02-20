package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"golang.org/x/sync/errgroup"
)

const (
	defaultEnvironmentSyncConcurrency = 4
	defaultEnvironmentSyncTimeout     = 90 * time.Second
)

type healthEnvironment struct {
	ID      string
	Name    string
	Enabled bool
}

type EnvironmentHealthJob struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	syncConcurrency    int
	syncTimeout        time.Duration
	running            atomic.Bool
}

func NewEnvironmentHealthJob(environmentService *services.EnvironmentService, settingsService *services.SettingsService) *EnvironmentHealthJob {
	return &EnvironmentHealthJob{
		environmentService: environmentService,
		settingsService:    settingsService,
		syncConcurrency:    defaultEnvironmentSyncConcurrency,
		syncTimeout:        defaultEnvironmentSyncTimeout,
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
	if !j.running.CompareAndSwap(false, true) {
		slog.WarnContext(ctx, "environment health check skipped; previous run still in progress")
		return
	}
	defer j.running.Store(false)

	slog.InfoContext(ctx, "environment health check started")

	// Get all environments using the DB directly
	db := j.environmentService.GetDB()
	var environments []healthEnvironment

	if err := db.WithContext(ctx).
		Model(&healthEnvironment{}).
		Table("environments").
		Where("enabled = ?", true).
		Find(&environments).Error; err != nil {
		slog.ErrorContext(ctx, "failed to list environments for health check", "error", err)
		return
	}

	checkedCount := 0
	onlineCount := 0
	offlineCount := 0
	syncedRemoteCount := 0
	var onlineRemote []healthEnvironment

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
			// Queue sync for online remote environments (skip local environment ID "0")
			if env.ID != "0" {
				onlineRemote = append(onlineRemote, env)
				syncedRemoteCount++
			}
		default:
			offlineCount++
		}
	}

	j.syncOnlineRemoteEnvironments(ctx, onlineRemote)
	slog.InfoContext(ctx, "environment health check completed", "checked", checkedCount, "online", onlineCount, "offline", offlineCount, "remote_sync_queued", syncedRemoteCount)
}

func (j *EnvironmentHealthJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling environment health job in new scheduler; currently requires restart")
	return nil
}

func (j *EnvironmentHealthJob) syncOnlineRemoteEnvironments(ctx context.Context, environments []healthEnvironment) {
	if len(environments) == 0 {
		return
	}

	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), j.syncTimeout)
	defer cancel()

	g, groupCtx := errgroup.WithContext(syncCtx)
	if j.syncConcurrency > 0 {
		g.SetLimit(j.syncConcurrency)
	}

	for _, env := range environments {
		g.Go(func() error {
			if err := j.environmentService.SyncRegistriesToEnvironment(groupCtx, env.ID); err != nil {
				slog.WarnContext(groupCtx, "failed to sync registries during health check",
					"environment_id", env.ID,
					"environment_name", env.Name,
					"error", err)
				return nil
			}

			slog.DebugContext(groupCtx, "successfully synced registries during health check",
				"environment_id", env.ID,
				"environment_name", env.Name)
			return nil
		})

		g.Go(func() error {
			if err := j.environmentService.SyncRepositoriesToEnvironment(groupCtx, env.ID); err != nil {
				slog.WarnContext(groupCtx, "failed to sync git repositories during health check",
					"environment_id", env.ID,
					"environment_name", env.Name,
					"error", err)
				return nil
			}

			slog.DebugContext(groupCtx, "successfully synced git repositories during health check",
				"environment_id", env.ID,
				"environment_name", env.Name)
			return nil
		})
	}

	_ = g.Wait()
	if syncCtx.Err() != nil {
		slog.WarnContext(ctx, "environment health sync phase timed out or canceled", "error", syncCtx.Err())
	}
}
