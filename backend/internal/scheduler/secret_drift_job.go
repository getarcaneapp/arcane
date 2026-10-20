package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/secretsource"
	"getarcane.dev/types/v3"
)

type (
	// SecretDriftCheckJob fetches every deployed project's bound secrets, records
	// whether they changed since the last deploy, and redeploys running projects
	// whose binding opts in to auto-redeploy. Each change triggers at most one
	// redeploy attempt. Internal job: no job_metadata entry.
	SecretDriftCheckJob struct {
		secrets  *secretsource.SecretSourceService
		projects *project.ProjectService
	}
)

const (
	// SecretDriftCheckJobName identifies the background check of bound project
	// secrets against what each project last deployed with.
	SecretDriftCheckJobName = "secret-drift-check"
)

func NewSecretDriftCheckJob(secrets *secretsource.SecretSourceService, projects *project.ProjectService) *SecretDriftCheckJob {
	return &SecretDriftCheckJob{secrets: secrets, projects: projects}
}

func (j *SecretDriftCheckJob) Name() string {
	return SecretDriftCheckJobName
}

func (j *SecretDriftCheckJob) Schedule(_ context.Context) string {
	// Every five minutes, offset from jobs that fire on the hour.
	return "2-59/5 * * * *"
}

func (j *SecretDriftCheckJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	if j.secrets == nil || j.projects == nil {
		return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
	}

	changes := j.secrets.CheckDrift(ctx, j.projects.SecretProjectRef)
	var errs []error
	for _, change := range changes {
		if !change.AutoRedeploy {
			continue
		}
		proj, err := j.projects.GetProjectFromDatabaseByID(ctx, change.ProjectID)
		if err != nil {
			errs = append(errs, fmt.Errorf("project %s: %w", change.ProjectID, err))
			continue
		}
		if proj.Status != project.ProjectStatusRunning {
			slog.InfoContext(ctx, "Secrets changed for a project that is not running; not redeploying",
				"jobName", SecretDriftCheckJobName, "projectId", change.ProjectID, "status", string(proj.Status))
			continue
		}
		slog.InfoContext(ctx, "Redeploying project after its secrets changed", "jobName", SecretDriftCheckJobName, "projectId", change.ProjectID)
		if deployErr := j.projects.DeployProject(ctx, change.ProjectID, types.SystemUser, nil); deployErr != nil {
			errs = append(errs, fmt.Errorf("redeploy %s: %w", change.ProjectName, deployErr))
		}
	}

	if joined := errors.Join(errs...); joined != nil {
		slog.WarnContext(ctx, "Secret drift check finished with errors", "jobName", SecretDriftCheckJobName, "error", joined)
		return types.SchedulerOutcome{Status: types.RunStatusPartial}, joined
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}

func (j *SecretDriftCheckJob) Reschedule(_ context.Context) error {
	return nil
}
