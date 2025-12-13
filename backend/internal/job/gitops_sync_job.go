package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/go-co-op/gocron/v2"
)

type GitOpsSyncJob struct {
	syncService     *services.GitOpsSyncService
	settingsService *services.SettingsService
	scheduler       *Scheduler
}

func NewGitOpsSyncJob(scheduler *Scheduler, syncService *services.GitOpsSyncService, settingsService *services.SettingsService) *GitOpsSyncJob {
	return &GitOpsSyncJob{
		syncService:     syncService,
		settingsService: settingsService,
		scheduler:       scheduler,
	}
}

func (j *GitOpsSyncJob) Register(ctx context.Context) error {
	// Check if GitOps sync is enabled via settings
	gitopsSyncEnabled := j.settingsService.GetBoolSetting(ctx, "gitopsSyncEnabled", true)

	if !gitopsSyncEnabled {
		slog.InfoContext(ctx, "GitOps sync disabled; job not registered")
		return nil
	}

	// Default interval: 5 minutes
	interval := 5 * time.Minute

	slog.InfoContext(ctx, "registering GitOps sync job", "interval", interval.String())

	// Ensure single instance
	j.scheduler.RemoveJobByName("gitops-sync")

	jobDefinition := gocron.DurationJob(interval)
	return j.scheduler.RegisterJob(
		ctx,
		"gitops-sync",
		jobDefinition,
		j.Execute,
		false,
	)
}

func (j *GitOpsSyncJob) Execute(ctx context.Context) error {
	slog.InfoContext(ctx, "GitOps sync run started")

	enabled := j.settingsService.GetBoolSetting(ctx, "gitopsSyncEnabled", true)
	if !enabled {
		slog.InfoContext(ctx, "GitOps sync disabled; skipping run")
		return nil
	}

	if err := j.syncService.SyncAllEnabled(ctx); err != nil {
		slog.ErrorContext(ctx, "GitOps sync run failed", "err", err)
		return err
	}

	slog.InfoContext(ctx, "GitOps sync run completed")
	return nil
}
