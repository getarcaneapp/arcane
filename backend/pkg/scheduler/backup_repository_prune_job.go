package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
)

// BackupRepositoryPruneJobName identifies the periodic prune of the local backup repositories.
const BackupRepositoryPruneJobName = "backup-repository-prune"

// BackupRepositoryPruneJob removes packs that deleted backups left marked for deletion.
// Internal job: no job_metadata entry, invisible in the Jobs UI.
type BackupRepositoryPruneJob struct {
	systemService *system.SystemService
	volumeService *volume.VolumeService
}

// NewBackupRepositoryPruneJob builds the prune job for the scheduler.
func NewBackupRepositoryPruneJob(systemService *system.SystemService, volumeService *volume.VolumeService) *BackupRepositoryPruneJob {
	return &BackupRepositoryPruneJob{systemService: systemService, volumeService: volumeService}
}

// Name returns the job name.
func (j *BackupRepositoryPruneJob) Name() string {
	return BackupRepositoryPruneJobName
}

// Schedule runs the prune twice a day, staggered after the default 03:00
// system backup schedule. Rustic keeps marked packs for 23 hours, so a daily
// run could leave deleted data on disk for almost two days.
func (j *BackupRepositoryPruneJob) Schedule(_ context.Context) string {
	return "0 15 4,16 * * *"
}

// Run prunes the local system and volume backup repositories.
func (j *BackupRepositoryPruneJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	var pruneErr error
	if err := j.systemService.PruneLocalBackupRepository(ctx); err != nil {
		pruneErr = fmt.Errorf("prune system backup repository: %w", err)
	}
	if err := j.volumeService.PruneLocalBackupRepository(ctx); err != nil {
		pruneErr = errors.Join(pruneErr, fmt.Errorf("prune volume backup repository: %w", err))
	}
	if pruneErr != nil {
		slog.ErrorContext(ctx, "Failed to prune local backup repositories", "jobName", BackupRepositoryPruneJobName, "error", pruneErr)
		return schedulertypes.Outcome{}, pruneErr
	}
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}

// Reschedule is a no-op: the schedule is fixed.
func (j *BackupRepositoryPruneJob) Reschedule(_ context.Context) error {
	return nil
}
