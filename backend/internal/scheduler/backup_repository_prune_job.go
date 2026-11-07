package scheduler

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/types/v3"
)

type (
	// Internal job: no job_metadata entry, invisible in the Jobs UI.
	BackupRepositoryPruneJob struct {
		systemService   *system.SystemService
		settingsService *settings.SettingsService
	}
)

const (
	BackupRepositoryPruneJobName = "backup-repository-prune"
)

func NewBackupRepositoryPruneJob(systemService *system.SystemService, settingsService *settings.SettingsService) *BackupRepositoryPruneJob {
	return &BackupRepositoryPruneJob{
		systemService:   systemService,
		settingsService: settingsService,
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
	days := j.settingsService.GetIntSetting(ctx, "unmanagedBackupRetentionDays")
	if err := j.systemService.PruneLocalBackupRepository(ctx, types.UnmanagedBackupSweep{
		Now:       time.Now(),
		Retention: time.Duration(days) * 24 * time.Hour,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to prune local backup repositories", "jobName", BackupRepositoryPruneJobName, "error", err)
		return types.SchedulerOutcome{}, fmt.Errorf("prune backup repositories: %w", err)
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *BackupRepositoryPruneJob) Reschedule(_ context.Context) error {
	return nil
}
