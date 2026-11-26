package scheduler

import (
	"context"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/types/v3"
)

type (
	ProjectBackupDiscoveryJob struct {
		projects *project.Module
	}
)

const (
	ProjectBackupDiscoveryJobName = "project-backup-discovery"
)

func NewProjectBackupDiscoveryJob(projects *project.Module) *ProjectBackupDiscoveryJob {
	return &ProjectBackupDiscoveryJob{projects: projects}
}

func (j *ProjectBackupDiscoveryJob) Name() string {
	return ProjectBackupDiscoveryJobName
}

func (j *ProjectBackupDiscoveryJob) Schedule(_ context.Context) string {
	return "45 * * * *"
}

func (j *ProjectBackupDiscoveryJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	if err := j.projects.DiscoverBackupsAfterRestore(ctx); err != nil {
		slog.WarnContext(ctx, "Project backup discovery after a system restore failed", "jobName", ProjectBackupDiscoveryJobName, "error", err)
		return types.SchedulerOutcome{}, err
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}
