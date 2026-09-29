package scheduler

import (
	"context"
	"log/slog"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// BackupRepositoryPruneJobName identifies the daily prune of the local backup
// repositories.
const BackupRepositoryPruneJobName = "backup-repository-prune"

// BackupRepositoryPruneJob frees the disk space deleted backups leave behind.
// Deleting a backup only marks its data for deletion, and rustic removes
// marked packs on a later prune once its keep-delete window has passed.
// Internal job: no job_metadata entry, invisible in the Jobs UI.
type BackupRepositoryPruneJob struct {
	systemBackups *systembackup.SystemBackupService
	volumes       *volume.VolumeService
}

// NewBackupRepositoryPruneJob builds the prune job for the scheduler.
func NewBackupRepositoryPruneJob(systemBackups *systembackup.SystemBackupService, volumes *volume.VolumeService) *BackupRepositoryPruneJob {
	return &BackupRepositoryPruneJob{systemBackups: systemBackups, volumes: volumes}
}

func (j *BackupRepositoryPruneJob) Name() string {
	return BackupRepositoryPruneJobName
}

func (j *BackupRepositoryPruneJob) Schedule(_ context.Context) string {
	return "0 15 4 * * *"
}

func (j *BackupRepositoryPruneJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	var pruneErr error
	if j.systemBackups != nil {
		if err := j.systemBackups.PruneLocalRepository(ctx); err != nil {
			pruneErr = errors.Combine(pruneErr, errors.WrapIf(err, "prune system backup repository"))
		}
	}
	if j.volumes != nil {
		if err := j.volumes.PruneLocalRepository(ctx); err != nil {
			pruneErr = errors.Combine(pruneErr, errors.WrapIf(err, "prune volume backup repository"))
		}
	}
	if pruneErr != nil {
		slog.ErrorContext(ctx, "Failed to prune local backup repositories", "jobName", BackupRepositoryPruneJobName, "error", pruneErr)
		return schedulertypes.Outcome{}, pruneErr
	}
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}

func (j *BackupRepositoryPruneJob) Reschedule(_ context.Context) error {
	return nil
}
