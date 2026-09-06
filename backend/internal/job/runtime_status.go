package job

import (
	"context"
	"slices"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

func (s *JobService) addRuntimeStatusInternal(ctx context.Context, environmentID string, jobs *[]jobschedule.JobStatus) error {
	runsByJob := make(map[string][]st.Run)
	if s.Queue != nil {
		records, err := s.Queue.Records(ctx)
		if err != nil {
			return errors.WrapIf(err, "load durable job status")
		}
		for _, record := range records {
			if record.EnvironmentID == environmentID {
				runsByJob[record.JobID] = append(runsByJob[record.JobID], record.Runs...)
			}
		}
	}
	runtime, hasRuntime := s.scheduler.(st.JobScheduler)
	if environmentID == "0" && hasRuntime {
		appendDynamicJobStatusesInternal(ctx, runtime, jobs)
	}

	for index := range *jobs {
		status := &(*jobs)[index]
		applyRunStatusInternal(status, runsByJob[status.ID])
		if environmentID == "0" && hasRuntime {
			setRuntimeHealthInternal(ctx, runtime, status)
		}
		for childIndex := range status.Children {
			child := &status.Children[childIndex]
			applyRunStatusInternal(child, runsByJob[child.ID])
		}
	}
	return nil
}

func appendDynamicJobStatusesInternal(ctx context.Context, runtime st.JobScheduler, jobs *[]jobschedule.JobStatus) {
	parentIndices := make(map[string]int, len(*jobs))
	for index, status := range *jobs {
		parentIndices[status.ID] = index
	}
	registered := runtime.ListRegisteredJobs()
	slices.SortFunc(registered, func(left, right st.Job) int {
		return strings.Compare(left.Name(), right.Name())
	})
	for _, registeredJob := range registered {
		parentID, _, dynamic := strings.Cut(registeredJob.Name(), ":")
		if !dynamic {
			continue
		}
		if parentID == "environment-health" {
			if registeredJob.Name() == "environment-health:0" {
				if index, exists := parentIndices[parentID]; exists {
					health := dynamicJobStatusInternal(ctx, runtime, registeredJob, "monitoring", true)
					health.Name = (*jobs)[index].Name
					(*jobs)[index] = health
				}
			}
			continue
		}
		category, managerOnly, supported := dynamicJobCategoryInternal(parentID)
		if !supported {
			continue
		}
		parentIndex, exists := parentIndices[parentID]
		if !exists {
			parentIndex = len(*jobs)
			parentIndices[parentID] = parentIndex
			*jobs = append(*jobs, jobschedule.JobStatus{
				ID:            parentID,
				Name:          parentID,
				Category:      category,
				Enabled:       true,
				ManagerOnly:   managerOnly,
				Prerequisites: []jobschedule.JobPrerequisite{},
			})
		}
		parent := &(*jobs)[parentIndex]
		duplicate := slices.ContainsFunc(parent.Children, func(child jobschedule.JobStatus) bool {
			return child.ID == registeredJob.Name()
		})
		if duplicate {
			continue
		}
		parent.Children = append(parent.Children, dynamicJobStatusInternal(ctx, runtime, registeredJob, category, managerOnly))
	}
}

func dynamicJobCategoryInternal(parentID string) (category string, managerOnly, supported bool) {
	switch parentID {
	case "environment-health":
		return "monitoring", true, true
	case "gitops-sync":
		return "sync", false, true
	case "system-backup":
		return "maintenance", true, true
	case "volume-backup":
		return "maintenance", false, true
	default:
		return "", false, false
	}
}

func dynamicJobStatusInternal(ctx context.Context, runtime st.JobScheduler, job st.Job, category string, managerOnly bool) jobschedule.JobStatus {
	child := jobschedule.JobStatus{
		ID:             job.Name(),
		Name:           job.Name(),
		Category:       category,
		Schedule:       job.Schedule(ctx),
		Enabled:        true,
		ManagerOnly:    managerOnly,
		CanRunManually: true,
		Prerequisites:  []jobschedule.JobPrerequisite{},
	}
	if conditional, ok := job.(st.ConditionalJob); ok {
		child.Enabled = conditional.ShouldSchedule(ctx)
	}
	if state, ok := runtime.GetJobRuntimeState(child.ID); ok {
		child.Schedule = state.Schedule
		child.NextRun = state.NextRun
	}
	return child
}

func applyRunStatusInternal(status *jobschedule.JobStatus, runs []st.Run) {
	var errorAt time.Time
	if status.LastError != "" {
		for _, run := range []*st.Run{status.CurrentRun, status.LastRun} {
			if run != nil && run.UpdatedAt.After(errorAt) {
				errorAt = run.UpdatedAt
			}
		}
	}
	for _, run := range runs {
		updateLatestRunInternal(status, run)
		updateLastSuccessInternal(status, run)
		if run.UpdatedAt.After(errorAt) && run.Outcome.Message != "" && runHasErrorInternal(run.Status) {
			status.LastError = run.Outcome.Message
			errorAt = run.UpdatedAt
		}
	}
}

func updateLatestRunInternal(status *jobschedule.JobStatus, run st.Run) {
	if !run.Status.Terminal() {
		if status.CurrentRun == nil || preferCurrentRunInternal(run, *status.CurrentRun) {
			status.CurrentRun = new(run)
		}
		return
	}
	current := status.CurrentRun
	if current != nil && current.ID == run.ID && run.UpdatedAt.After(current.UpdatedAt) && (!run.RemoteAccepted || run.RemoteSettled) {
		status.CurrentRun = nil
	}
	if status.LastRun == nil || run.UpdatedAt.After(status.LastRun.UpdatedAt) {
		status.LastRun = new(run)
	}
}

func updateLastSuccessInternal(status *jobschedule.JobStatus, run st.Run) {
	if run.Status != st.Succeeded {
		return
	}
	succeededAt := run.UpdatedAt
	if run.FinishedAt != nil {
		succeededAt = *run.FinishedAt
	}
	if status.LastSuccess == nil || succeededAt.After(*status.LastSuccess) {
		status.LastSuccess = new(succeededAt)
	}
}

func runHasErrorInternal(status st.RunStatus) bool {
	switch status {
	case st.Failed, st.Partial, st.NeedsAttention, st.Retrying, st.Waiting:
		return true
	case st.Queued, st.Running, st.Succeeded, st.Skipped, st.Canceled:
		return false
	default:
		return false
	}
}

func preferCurrentRunInternal(candidate, current st.Run) bool {
	if candidate.ID == current.ID {
		return candidate.UpdatedAt.After(current.UpdatedAt)
	}
	candidatePriority := currentRunPriorityInternal(candidate.Status)
	currentPriority := currentRunPriorityInternal(current.Status)
	if candidatePriority != currentPriority {
		return candidatePriority < currentPriority
	}
	return candidate.CreatedAt.Before(current.CreatedAt)
}

func currentRunPriorityInternal(status st.RunStatus) int {
	switch status {
	case st.Running:
		return 0
	case st.NeedsAttention:
		return 1
	case st.Queued, st.Waiting, st.Retrying, st.Succeeded, st.Partial, st.Skipped, st.Failed, st.Canceled:
		return 2
	default:
		return 2
	}
}

func setRuntimeHealthInternal(ctx context.Context, runtime st.JobScheduler, status *jobschedule.JobStatus) {
	if health, found := runtime.WatcherHealth(status.ID); found {
		status.WorkerHealth = &health
		return
	}
	registered, found := runtime.GetJob(status.ID)
	if !found {
		return
	}
	if conditional, ok := registered.(st.ConditionalJob); ok && !conditional.ShouldSchedule(ctx) {
		return
	}
	state, found := runtime.GetJobRuntimeState(status.ID)
	if !found || !state.Scheduled {
		status.NextRun = nil
		status.WorkerHealth = &st.WorkerHealth{Status: "needs_attention", LastError: "Job has no installed schedule", UpdatedAt: time.Now().UTC()}
	}
}
