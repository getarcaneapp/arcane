package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/robfig/cron/v3"
)

type GitOpsSyncJob struct {
	syncService     *services.GitOpsSyncService
	settingsService *services.SettingsService
}

func NewGitOpsSyncJob(syncService *services.GitOpsSyncService, settingsService *services.SettingsService) *GitOpsSyncJob {
	return &GitOpsSyncJob{
		syncService:     syncService,
		settingsService: settingsService,
	}
}

func (j *GitOpsSyncJob) Name() string {
	return "gitops-sync"
}

func (j *GitOpsSyncJob) Schedule(ctx context.Context) string {
	schedule := j.settingsService.GetStringSetting(ctx, "gitopsSyncInterval", "0 */1 * * * *")
	if schedule == "" {
		schedule = "0 */1 * * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(schedule); err == nil {
		if i <= 0 {
			i = 1
		}
		if i%60 == 0 {
			schedule = fmt.Sprintf("0 0 */%d * * *", i/60)
		} else {
			schedule = fmt.Sprintf("0 */%d * * * *", i)
		}
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for gitops-sync, using default", "invalid_schedule", schedule, "error", err)
		return "0 */1 * * * *"
	}

	return schedule
}

func (j *GitOpsSyncJob) Run(ctx context.Context) {
	enabled := j.settingsService.GetBoolSetting(ctx, "gitopsSyncEnabled", true)
	if !enabled {
		slog.DebugContext(ctx, "GitOps sync disabled; skipping run")
		return
	}

	slog.InfoContext(ctx, "GitOps sync run started")

	if err := j.syncService.SyncAllEnabled(ctx); err != nil {
		slog.ErrorContext(ctx, "GitOps sync run failed", "err", err)
		return
	}

	slog.InfoContext(ctx, "GitOps sync run completed")
}
