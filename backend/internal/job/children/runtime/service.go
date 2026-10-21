// Package runtime projects durable run history and scheduler runtime state
// onto job statuses.
package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

// ApplyStatuses merges durable runs for one environment into jobs. The local
// environment also gains dynamic children and scheduler worker health.
func ApplyStatuses(ctx context.Context, coordinator *runs.Coordinator, controller types.JobController, environmentID string, jobs []types.JobStatus) ([]types.JobStatus, error) {
	records, err := coordinator.Records(ctx)
	if err != nil {
		return nil, fmt.Errorf("load durable job status: %w", err)
	}
	runsByJob := make(map[string][]types.SchedulerRun)
	for _, record := range records {
		if record.EnvironmentID == environmentID {
			runsByJob[record.JobID] = append(runsByJob[record.JobID], record.Runs...)
		}
	}
	jobScheduler, hasScheduler := controller.(types.JobScheduler)
	jobs, dynamicChildren := appendDynamicJobStatuses(ctx, controller, environmentID, jobs)

	statuses := make([]types.JobStatus, 0, len(jobs))
	for _, job := range jobs {
		status := ApplyRunStatus(job, runsByJob[job.ID])
		workerHealth, nextRun := func() (*types.SchedulerWorkerHealth, *time.Time) {
			if environmentID != "0" || !hasScheduler {
				return status.WorkerHealth, status.NextRun
			}
			if health, found := jobScheduler.WatcherHealth(status.ID); found {
				return &health, status.NextRun
			}
			registered, found := jobScheduler.GetJob(status.ID)
			if !found {
				return status.WorkerHealth, status.NextRun
			}
			if conditional, ok := registered.(types.ConditionalJob); ok && !conditional.ShouldSchedule(ctx) {
				return status.WorkerHealth, status.NextRun
			}
			if state, stateFound := jobScheduler.GetJobRuntimeState(status.ID); !stateFound || !state.Scheduled {
				return &types.SchedulerWorkerHealth{
					Status:    "needs_attention",
					LastError: "Job has no installed schedule",
					UpdatedAt: time.Now().UTC(),
				}, nil
			}
			return status.WorkerHealth, status.NextRun
		}()
		var children []types.JobStatus
		for _, child := range slices.Concat(status.Children, dynamicChildren[status.ID]) {
			children = append(children, ApplyRunStatus(child, runsByJob[child.ID]))
		}
		statuses = append(statuses, types.JobStatus{ //exhaustruct:enforce
			CurrentRun:     status.CurrentRun,
			LastRun:        status.LastRun,
			LastSuccess:    status.LastSuccess,
			LastError:      status.LastError,
			WorkerHealth:   workerHealth,
			Children:       children,
			ID:             status.ID,
			Name:           status.Name,
			Description:    status.Description,
			Category:       status.Category,
			Schedule:       status.Schedule,
			NextRun:        nextRun,
			Enabled:        status.Enabled,
			ManagerOnly:    status.ManagerOnly,
			IsContinuous:   status.IsContinuous,
			CanRunManually: status.CanRunManually,
			Prerequisites:  status.Prerequisites,
			SettingsKey:    status.SettingsKey,
		})
	}
	return statuses, nil
}

func runExecutionTime(run types.SchedulerRun) time.Time {
	startedAt := run.CreatedAt
	if run.StartedAt != nil && run.StartedAt.After(startedAt) {
		startedAt = *run.StartedAt
	}
	// Manager delivery attempts poll the agent; they are not job executions.
	if run.EnvironmentID == "0" || !run.RemoteAccepted {
		for _, attempt := range run.Attempts {
			if attempt.StartedAt.After(startedAt) {
				startedAt = attempt.StartedAt
			}
		}
	}
	return startedAt
}

func ProjectRunOutcome(run types.SchedulerRun) types.SchedulerRun {
	// Delivery reconciliation must not hide a failure already reported by the agent.
	agentFailure := kit.FromPtr(run.RemoteOutcome).Status == types.RunStatusNeedsAttention && !run.RemoteRetryRequested
	failed := agentFailure || run.Status == types.RunStatusFailed || run.Status == types.RunStatusNeedsAttention
	outcome := kit.Ternary(agentFailure, kit.FromPtr(run.RemoteOutcome), run.Outcome)
	return types.SchedulerRun{ //exhaustruct:enforce
		ActivityEnvironmentID: run.ActivityEnvironmentID,
		ActivityID:            run.ActivityID,
		RequestedWithKey:      run.RequestedWithKey,
		ID:                    run.ID,
		JobID:                 run.JobID,
		EnvironmentID:         run.EnvironmentID,
		Trigger:               run.Trigger,
		RequestedBy:           run.RequestedBy,
		Status:                kit.Ternary(failed, types.RunStatusFailed, run.Status),
		CreatedAt:             run.CreatedAt,
		UpdatedAt:             run.UpdatedAt,
		StartedAt:             run.StartedAt,
		FinishedAt:            run.FinishedAt,
		NextAttempt:           run.NextAttempt,
		AttemptCount:          run.AttemptCount,
		Owner:                 run.Owner,
		Outcome: types.SchedulerOutcome{ //exhaustruct:enforce
			Status:     kit.Ternary(failed, types.RunStatusFailed, outcome.Status),
			Message:    outcome.Message,
			ActivityID: outcome.ActivityID,
			Targets:    outcome.Targets,
			Steps:      outcome.Steps,
		},
		Attempts:                run.Attempts,
		RemoteDeliveryAttempted: run.RemoteDeliveryAttempted,
		RemoteOutcome:           run.RemoteOutcome,
		RemoteRetryRequested:    run.RemoteRetryRequested,
		RemoteRetryAttempted:    run.RemoteRetryAttempted,
		RemoteAttemptCount:      run.RemoteAttemptCount,
		RemoteAccepted:          run.RemoteAccepted,
		RemoteSettled:           run.RemoteSettled,
		LastConfirmedAt:         run.LastConfirmedAt,
	}
}

func ApplyRunStatus(status types.JobStatus, jobRuns []types.SchedulerRun) types.JobStatus {
	latest := make(map[string]types.SchedulerRun, len(jobRuns)+2)
	for _, run := range []*types.SchedulerRun{status.CurrentRun, status.LastRun} {
		if run != nil {
			if previous, found := latest[run.ID]; !found || !run.UpdatedAt.Before(previous.UpdatedAt) {
				latest[run.ID] = *run
			}
		}
	}
	for _, run := range jobRuns {
		if previous, found := latest[run.ID]; !found || !run.UpdatedAt.Before(previous.UpdatedAt) {
			latest[run.ID] = run
		}
	}
	priority := func(runStatus types.RunStatus) int {
		switch runStatus {
		case types.RunStatusRunning:
			return 0
		case types.RunStatusQueued, types.RunStatusWaiting, types.RunStatusRetrying:
			return 1
		case types.RunStatusNeedsAttention, types.RunStatusSucceeded, types.RunStatusPartial, types.RunStatusSkipped, types.RunStatusFailed, types.RunStatusCanceled:
			return 2
		default:
			return 2
		}
	}
	preferCurrent := func(candidate, current types.SchedulerRun) bool {
		switch {
		case candidate.ID == current.ID:
			return candidate.UpdatedAt.After(current.UpdatedAt)
		case priority(candidate.Status) != priority(current.Status):
			return priority(candidate.Status) < priority(current.Status)
		case candidate.CreatedAt.Equal(current.CreatedAt):
			return candidate.ID > current.ID
		default:
			return candidate.CreatedAt.After(current.CreatedAt)
		}
	}
	var currentRun, lastRun *types.SchedulerRun
	lastSuccess := status.LastSuccess
	for _, latestRun := range latest {
		run := ProjectRunOutcome(latestRun)
		switch {
		case !run.Status.Terminal() && run.Status != types.RunStatusNeedsAttention:
			if currentRun == nil || preferCurrent(run, *currentRun) {
				currentRun = new(run)
			}
		case lastRun == nil || runExecutionTime(run).After(runExecutionTime(*lastRun)) ||
			(runExecutionTime(run).Equal(runExecutionTime(*lastRun)) && run.ID > lastRun.ID):
			lastRun = new(run)
		}
		if run.Status == types.RunStatusSucceeded {
			succeededAt := kit.Ternary(run.FinishedAt != nil, kit.FromPtr(run.FinishedAt), runExecutionTime(run))
			if lastSuccess == nil || succeededAt.After(*lastSuccess) {
				lastSuccess = new(succeededAt)
			}
		}
	}
	errorRun := kit.Ternary(lastRun != nil && (currentRun == nil || runExecutionTime(*lastRun).After(runExecutionTime(*currentRun))), lastRun, currentRun)
	return types.JobStatus{ //exhaustruct:enforce
		CurrentRun:  currentRun,
		LastRun:     lastRun,
		LastSuccess: lastSuccess,
		LastError: kit.Ternary(
			slices.Contains([]types.RunStatus{types.RunStatusFailed, types.RunStatusPartial, types.RunStatusNeedsAttention, types.RunStatusRetrying}, kit.FromPtr(errorRun).Status),
			kit.FromPtr(errorRun).Outcome.Message,
			"",
		),
		WorkerHealth:   status.WorkerHealth,
		Children:       status.Children,
		ID:             status.ID,
		Name:           status.Name,
		Description:    status.Description,
		Category:       status.Category,
		Schedule:       status.Schedule,
		NextRun:        status.NextRun,
		Enabled:        status.Enabled,
		ManagerOnly:    status.ManagerOnly,
		IsContinuous:   status.IsContinuous,
		CanRunManually: status.CanRunManually,
		Prerequisites:  status.Prerequisites,
		SettingsKey:    status.SettingsKey,
	}
}

func appendDynamicJobStatuses(
	ctx context.Context,
	controller types.JobController,
	environmentID string,
	jobs []types.JobStatus,
) ([]types.JobStatus, map[string][]types.JobStatus) {
	jobScheduler, hasScheduler := controller.(types.JobScheduler)
	if environmentID != "0" || !hasScheduler {
		return jobs, nil
	}
	registered := jobScheduler.ListRegisteredJobs()
	slices.SortFunc(registered, func(left, right types.SchedulerJob) int {
		return strings.Compare(left.Name(), right.Name())
	})
	dynamicChildren := make(map[string][]types.JobStatus)
	for _, registeredJob := range registered {
		parentID, _, dynamic := strings.Cut(registeredJob.Name(), ":")
		parentIndex := slices.IndexFunc(jobs, func(job types.JobStatus) bool { return job.ID == parentID })
		if !dynamic || parentID == "environment-health" {
			if registeredJob.Name() == "environment-health:0" && parentIndex >= 0 {
				jobs[parentIndex] = dynamicJobStatus(ctx, jobScheduler, registeredJob, jobs[parentIndex].Name, "monitoring", true)
			}
			continue
		}
		category, managerOnly, supported := func() (string, bool, bool) {
			switch parentID {
			case "gitops-sync":
				return "sync", false, true
			case "system-backup":
				return "maintenance", true, true
			case "volume-backup":
				return "maintenance", false, true
			default:
				return "", false, false
			}
		}()
		if !supported {
			continue
		}
		if parentIndex < 0 {
			parentIndex = len(jobs)
			jobs = append(jobs, types.JobStatus{
				ID:            parentID,
				Name:          parentID,
				Category:      category,
				Enabled:       true,
				ManagerOnly:   managerOnly,
				Prerequisites: []types.JobPrerequisite{},
			})
		}
		duplicate := slices.ContainsFunc(jobs[parentIndex].Children, func(child types.JobStatus) bool {
			return child.ID == registeredJob.Name()
		})
		if !duplicate {
			dynamicChildren[parentID] = append(dynamicChildren[parentID], dynamicJobStatus(ctx, jobScheduler, registeredJob, registeredJob.Name(), category, managerOnly))
		}
	}
	return jobs, dynamicChildren
}

func dynamicJobStatus(ctx context.Context, jobScheduler types.JobScheduler, job types.SchedulerJob, name, category string, managerOnly bool) types.JobStatus {
	state, scheduled := jobScheduler.GetJobRuntimeState(job.Name())
	return types.JobStatus{
		ID:       job.Name(),
		Name:     name,
		Category: category,
		Schedule: kit.Ternary(scheduled, state.Schedule, job.Schedule(ctx)),
		NextRun:  state.NextRun,
		Enabled: func() bool {
			conditional, ok := job.(types.ConditionalJob)
			return !ok || conditional.ShouldSchedule(ctx)
		}(),
		ManagerOnly:    managerOnly,
		CanRunManually: true,
		Prerequisites:  []types.JobPrerequisite{},
	}
}
