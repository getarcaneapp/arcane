package scheduler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"

	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/types/v3"
)

type (
	// Internal job: no job_metadata entry, invisible in the Jobs UI.
	BackupRepositoryPruneJob struct {
		systemService   *system.SystemService
		volumeService   *volume.VolumeService
		projects        *project.Module
		settingsService *settings.SettingsService
	}

	BackupRepositoryPruneJobParams struct {
		fx.In

		SystemService   *system.SystemService
		VolumeService   *volume.VolumeService
		Projects        *project.Module
		SettingsService *settings.SettingsService
	}
)

const (
	BackupRepositoryPruneJobName = "backup-repository-prune"
)

func NewBackupRepositoryPruneJob(p BackupRepositoryPruneJobParams) *BackupRepositoryPruneJob {
	return &BackupRepositoryPruneJob{
		systemService:   p.SystemService,
		volumeService:   p.VolumeService,
		projects:        p.Projects,
		settingsService: p.SettingsService,
	}
}

func (j *BackupRepositoryPruneJob) Name() string {
	return BackupRepositoryPruneJobName
}

func (j *BackupRepositoryPruneJob) Schedule(ctx context.Context) string {
	instance := fnv.New32a()
	_, _ = instance.Write([]byte(j.settingsService.GetStringSetting(ctx, "instanceId")))
	return fmt.Sprintf("%d 4,16 * * *", instance.Sum32()%60)
}

func (j *BackupRepositoryPruneJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	var pruneErr error
	if err := j.systemService.PruneLocalBackupRepository(ctx); err != nil {
		pruneErr = fmt.Errorf("prune system backup repository: %w", err)
	}
	if err := j.projects.PruneUnmanagedBackups(ctx); err != nil {
		pruneErr = errors.Join(pruneErr, fmt.Errorf("prune unmanaged project backups: %w", err))
	}
	if err := j.volumeService.PruneLocalBackupRepository(ctx); err != nil {
		pruneErr = errors.Join(pruneErr, fmt.Errorf("prune volume backup repository: %w", err))
	}
	if pruneErr != nil {
		slog.ErrorContext(ctx, "Failed to prune local backup repositories", "jobName", BackupRepositoryPruneJobName, "error", pruneErr)
		return types.SchedulerOutcome{}, pruneErr
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *BackupRepositoryPruneJob) Reschedule(_ context.Context) error {
	return nil
}
