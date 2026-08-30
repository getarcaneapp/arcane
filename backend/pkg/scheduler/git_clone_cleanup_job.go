package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

// GitCloneCleanupJobName identifies the hourly purge of leaked git clone
// scratch directories.
const GitCloneCleanupJobName = "git-clone-scratch-cleanup"

// gitCloneScratchMinAge is the base mtime cutoff for purging clone scratch dirs.
const gitCloneScratchMinAge = 2 * time.Hour

// gitCloneScratchBuildHold pads the configured build timeout.
const gitCloneScratchBuildHold = 30 * time.Minute

// GitCloneCleanupJob purges orphaned "gitops-*" clone dirs under the git work
// dir. Internal job: no job_metadata entry, invisible in the Jobs UI.
type GitCloneCleanupJob struct {
	repoService     *gitrepo.GitRepositoryService
	settingsService *settings.SettingsService
}

// NewGitCloneCleanupJob builds the cleanup job for the scheduler.
func NewGitCloneCleanupJob(repoService *gitrepo.GitRepositoryService, settingsService *settings.SettingsService) *GitCloneCleanupJob {
	return &GitCloneCleanupJob{repoService: repoService, settingsService: settingsService}
}

func (j *GitCloneCleanupJob) Name() string {
	return GitCloneCleanupJobName
}

func (j *GitCloneCleanupJob) Schedule(_ context.Context) string {
	// Staggered from the upload-sessions cleanup, which fires at minute 0.
	return "0 30 * * * *"
}

func (j *GitCloneCleanupJob) Run(ctx context.Context) {
	if j.repoService == nil || j.repoService.Client == nil {
		return
	}

	maxAge := gitCloneScratchMinAge
	if j.settingsService != nil {
		if buildTimeout := j.settingsService.GetSettingsConfig().BuildTimeout.AsDurationSeconds(); buildTimeout+gitCloneScratchBuildHold > maxAge {
			maxAge = buildTimeout + gitCloneScratchBuildHold
		}
	}
	removed, err := j.repoService.PurgeScratchDirs(ctx, maxAge)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to purge leaked git clone scratch dirs", "jobName", GitCloneCleanupJobName, "removed", removed, "error", err)
		return
	}
	if removed > 0 {
		slog.InfoContext(ctx, "Purged leaked git clone scratch dirs", "jobName", GitCloneCleanupJobName, "removed", removed)
	}
}

func (j *GitCloneCleanupJob) Reschedule(_ context.Context) error {
	return nil
}
