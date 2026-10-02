package runs

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	kit "go.getarcane.app/kit/pkg"
)

// SetObserver connects activity projection before accepting work.
func (q *Coordinator) SetObserver(observer scheduler.RunObserver) { q.observer = observer }

func (q *Coordinator) associateActivityInternal(run *scheduler.Run) {
	if q.observer != nil && run.ActivityID == "" {
		run.ActivityID = q.observer.ActivityID(*run)
	}
	if run.ActivityID != "" {
		run.ActivityEnvironmentID = run.EnvironmentID
	}
}

func (q *Coordinator) observeRunInternal(ctx context.Context, run scheduler.Run) {
	if q.observer == nil {
		return
	}
	q.observerMu.Lock()
	defer q.observerMu.Unlock()
	q.syncActivityInternal(ctx, run)
	q.signalInternal()
}

func (q *Coordinator) syncActivityInternal(ctx context.Context, run scheduler.Run) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var state scheduler.CoordinatorState
	id := kit.SHA256Hex(run.EnvironmentID + "\x00" + run.JobID)
	if err := q.service.GetState(ctx, coordinatorTypeInternal, id, &state); err != nil {
		return
	}
	version, pending := state.PendingActivitySync[run.ID]
	if !pending {
		return
	}
	current, err := q.Get(ctx, run.EnvironmentID, run.JobID, run.ID)
	if err == nil {
		err = q.observer.SyncRunActivity(ctx, current)
	}
	if err == nil {
		_, err = q.service.Invoke(ctx, coordinatorTypeInternal, id, "activity-synced", scheduler.ActivitySyncCommand{RunID: run.ID, UpdatedAt: version})
	}
	if err != nil {
		slog.ErrorContext(ctx, "Job activity synchronization failed; will retry", "runId", run.ID, "error", err)
	}
}

func (q *Coordinator) retryActivitySyncInternal(ctx context.Context) {
	if q.observer == nil {
		return
	}
	cursor := ""
	for {
		page, err := q.service.ListStates(ctx, coordinatorTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			slog.ErrorContext(ctx, "List pending job activities", "error", err)
			return
		}
		for _, entry := range page.States {
			var state scheduler.CoordinatorState
			if err := entry.Data.Decode(&state); err != nil {
				slog.ErrorContext(ctx, "Decode pending job activities", "error", err)
				continue
			}
			for runID := range state.PendingActivitySync {
				if ctx.Err() != nil {
					return
				}
				q.observerMu.Lock()
				q.syncActivityInternal(ctx, scheduler.Run{ID: runID, EnvironmentID: state.Record.EnvironmentID, JobID: state.Record.JobID})
				q.observerMu.Unlock()
			}
		}
		cursor = page.AfterID()
		if cursor == "" {
			return
		}
	}
}

func (q *Coordinator) hasPendingActivitySyncInternal(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor := ""
	for {
		page, err := q.service.ListStates(ctx, coordinatorTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return true
		}
		for _, entry := range page.States {
			var state scheduler.CoordinatorState
			if entry.Data.Decode(&state) != nil || len(state.PendingActivitySync) > 0 {
				return true
			}
		}
		cursor = page.AfterID()
		if cursor == "" {
			return false
		}
	}
}
