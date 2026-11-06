package scheduler

import (
	"context"
	"log/slog"
	"time"

	"getarcane.dev/arcane/v3/internal/gitrepo"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/types/v3"
)

type (
	GitCloneCleanupJob struct {
		repoService     *gitrepo.GitRepositoryService
		settingsService *settings.SettingsService
	}
)

const (
	GitCloneCleanupJobName   = "git-clone-scratch-cleanup"
	gitCloneScratchMinAge    = 2 * time.Hour
	gitCloneScratchBuildHold = 30 * time.Minute
)

func NewGitCloneCleanupJob(repoService *gitrepo.GitRepositoryService, settingsService *settings.SettingsService) *GitCloneCleanupJob {
	return &GitCloneCleanupJob{repoService: repoService, settingsService: settingsService}
}

func (j *GitCloneCleanupJob) Name() string {
	return GitCloneCleanupJobName
}

func (j *GitCloneCleanupJob) Schedule(_ context.Context) string {
	// Staggered from the upload-sessions cleanup, which fires at minute 0.
	return "30 * * * *"
}

func (j *GitCloneCleanupJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	maxAge := gitCloneScratchMinAge
	if buildTimeout := j.settingsService.GetSettingsConfig().BuildTimeout.AsDurationSeconds(); buildTimeout+gitCloneScratchBuildHold > maxAge {
		maxAge = buildTimeout + gitCloneScratchBuildHold
	}
	removed, err := j.repoService.PurgeScratchDirs(ctx, maxAge)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to purge leaked git clone scratch dirs", "jobName", GitCloneCleanupJobName, "removed", removed, "error", err)
		return types.SchedulerOutcome{}, err
	}
	if removed > 0 {
		slog.InfoContext(ctx, "Purged leaked git clone scratch dirs", "jobName", GitCloneCleanupJobName, "removed", removed)
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *GitCloneCleanupJob) Reschedule(_ context.Context) error {
	return nil
}
