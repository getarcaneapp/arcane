package runs

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
)

// SetObserver connects activity projection before accepting work.
func (q *Coordinator) SetObserver(observer types.RunObserver) { q.observer = observer }

func (q *Coordinator) associateActivity(run *types.SchedulerRun) {
	if q.observer != nil && run.ActivityID == "" {
		run.ActivityID = q.observer.ActivityID(*run)
	}
	if run.ActivityID != "" {
		run.ActivityEnvironmentID = run.EnvironmentID
	}
}

func (q *Coordinator) observeRun(ctx context.Context, run types.SchedulerRun) bool {
	if q.observer == nil {
		return false
	}
	defer q.observers.Lock(run.ID)()
	return q.syncActivity(ctx, run)
}

func (q *Coordinator) syncActivity(ctx context.Context, run types.SchedulerRun) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var state types.SchedulerCoordinatorState
	id := kit.SHA256Hex(run.EnvironmentID + "\x00" + run.JobID)
	if err := q.service.GetState(ctx, coordinatorType, id, &state); err != nil {
		return true
	}
	version, pending := state.PendingActivitySync[run.ID]
	if !pending {
		return false
	}
	current, err := q.Get(ctx, run.EnvironmentID, run.JobID, run.ID)
	if err == nil {
		err = q.observer.SyncRunActivity(ctx, current)
	}
	if err == nil {
		_, err = q.service.Invoke(ctx, coordinatorType, id, "activity-synced", types.SchedulerActivitySyncCommand{RunID: run.ID, UpdatedAt: version})
	}
	if err != nil {
		slog.ErrorContext(ctx, "Job activity synchronization failed; will retry", "runId", run.ID, "error", err)
		return true
	}
	return false
}

func (q *Coordinator) retryActivitySync(ctx context.Context, states []types.SchedulerCoordinatorState) bool {
	pending := false
	for _, state := range states {
		for runID := range state.PendingActivitySync {
			if ctx.Err() != nil {
				return true
			}
			pending = q.observeRun(ctx, types.SchedulerRun{ID: runID, EnvironmentID: state.Record.EnvironmentID, JobID: state.Record.JobID}) || pending
		}
	}
	return pending
}
