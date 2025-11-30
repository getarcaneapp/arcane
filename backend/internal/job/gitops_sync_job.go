package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/ofkm/arcane-backend/internal/services"
)

type GitOpsSyncJob struct {
	gitOpsService   *services.GitOpsRepositoryService
	settingsService *services.SettingsService
	scheduler       *Scheduler
}

func NewGitOpsSyncJob(scheduler *Scheduler, gitOpsService *services.GitOpsRepositoryService, settingsService *services.SettingsService) *GitOpsSyncJob {
	return &GitOpsSyncJob{
		gitOpsService:   gitOpsService,
		settingsService: settingsService,
		scheduler:       scheduler,
	}
}

func (j *GitOpsSyncJob) Register(ctx context.Context) error {
	gitOpsSyncEnabled := j.settingsService.GetBoolSetting(ctx, "gitOpsSyncEnabled", true)
	gitOpsSyncInterval := j.settingsService.GetIntSetting(ctx, "gitOpsSyncInterval", 5)

	if !gitOpsSyncEnabled {
		slog.InfoContext(ctx, "GitOps sync disabled; job not registered")
		return nil
	}

	interval := time.Duration(gitOpsSyncInterval) * time.Minute
	if interval < 1*time.Minute {
		slog.WarnContext(ctx, "GitOps sync interval too low; using default",
			slog.Int("requested_minutes", gitOpsSyncInterval),
			slog.String("effective_interval", "5m"))
		interval = 5 * time.Minute
	}

	slog.InfoContext(ctx, "registering GitOps sync job", slog.String("interval", interval.String()))

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

	results, err := j.gitOpsService.SyncAllEnabledRepositories(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "GitOps sync failed", slog.Any("err", err))
		return err
	}

	total := len(results)
	success := 0
	failed := 0

	for _, r := range results {
		if r.Success {
			success++
		} else {
			failed++
			slog.WarnContext(ctx, "Repository sync failed",
				slog.String("repository_id", r.RepositoryID),
				slog.String("url", r.URL),
				slog.String("error", r.Error))
		}
	}

	slog.InfoContext(ctx, "GitOps sync run completed",
		slog.Int("total", total),
		slog.Int("success", success),
		slog.Int("failed", failed))

	return nil
}

func (j *GitOpsSyncJob) Reschedule(ctx context.Context) error {
	gitOpsSyncEnabled := j.settingsService.GetBoolSetting(ctx, "gitOpsSyncEnabled", true)
	gitOpsSyncInterval := j.settingsService.GetIntSetting(ctx, "gitOpsSyncInterval", 5)

	if !gitOpsSyncEnabled {
		j.scheduler.RemoveJobByName("gitops-sync")
		slog.InfoContext(ctx, "GitOps sync disabled; removed gitops-sync job if present")
		return nil
	}

	interval := time.Duration(gitOpsSyncInterval) * time.Minute
	if interval < 1*time.Minute {
		interval = 5 * time.Minute
	}
	slog.InfoContext(ctx, "GitOps sync settings changed; rescheduling", slog.String("interval", interval.String()))

	return j.scheduler.RescheduleDurationJobByName(ctx, "gitops-sync", interval, j.Execute, false)
}
