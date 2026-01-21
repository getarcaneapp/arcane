package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/services"
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
	// Default interval: 1 minute to check for due syncs
	return "0 */1 * * * *"
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
