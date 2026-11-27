package runs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/italypaleale/francis/actor"

	kit "kit.getarcane.app"

	"getarcane.dev/types/v3"
)

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
	var state struct{ PendingActivitySync map[string]time.Time }
	id := kit.SHA256Hex(run.EnvironmentID + "\x00" + run.JobID)
	if err := q.service.GetState(ctx, coordinatorType, id, &state); err != nil {
		return true
	}
	if _, err := q.compactActivity(ctx, id, state.PendingActivitySync); err != nil {
		return true
	}
	version, pending := state.PendingActivitySync[run.ID]
	if !pending {
		return false
	}
	err := q.observer.SyncRunActivity(ctx, run)
	if err == nil {
		_, err = q.service.Invoke(ctx, coordinatorType, id, "activity-synced", types.SchedulerActivitySyncCommand{RunID: run.ID, UpdatedAt: version})
	}
	if err != nil {
		slog.ErrorContext(ctx, "Job activity synchronization failed; will retry", "runId", run.ID, "error", err)
		return true
	}
	return false
}

func (q *Coordinator) retryActivitySync(ctx context.Context, states []coordinatorSummary) bool {
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

func (q *Coordinator) compactActivity(ctx context.Context, id string, pending map[string]time.Time) (map[string]time.Time, error) {
	acknowledged := map[string]time.Time{}
	if err := q.service.GetState(ctx, activityAckType, id, &acknowledged); err != nil && !errors.Is(err, actor.ErrStateNotFound) {
		return nil, err
	}
	dropAcknowledged(pending, acknowledged)
	return acknowledged, nil
}

func (q *Coordinator) acknowledgements(ctx context.Context) (map[string]map[string]time.Time, error) {
	entries, err := listStates[map[string]time.Time](ctx, q.service, activityAckType)
	if err != nil {
		return nil, err
	}
	acknowledged := make(map[string]map[string]time.Time, len(entries))
	for _, entry := range entries {
		acknowledged[entry.ActorID] = entry.State
	}
	return acknowledged, nil
}

func dropAcknowledged(pending, acknowledged map[string]time.Time) {
	for runID, version := range acknowledged {
		if pendingVersion, found := pending[runID]; found && pendingVersion.Equal(version) {
			delete(pending, runID)
		}
	}
}
