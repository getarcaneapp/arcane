package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	kit "go.getarcane.app/kit/pkg"
)

// SetObserver connects activity projection before the queue starts accepting work.
func (q *Queue) SetObserver(observer scheduler.RunObserver) {
	q.observer = observer
}

func (q *Queue) associateActivityInternal(run *scheduler.Run) {
	if q.observer != nil && run.ActivityID == "" {
		run.ActivityID = q.observer.ActivityID(*run)
	}
	if run.ActivityID != "" {
		run.ActivityEnvironmentID = run.EnvironmentID
	}
}

func (q *Queue) observeRunInternal(ctx context.Context, run scheduler.Run) {
	if q.observer == nil {
		return
	}
	// Serialize repair bookkeeping with notifications. The observer rereads the
	// durable run, so delayed notifications cannot restore an older activity state.
	q.observerMu.Lock()
	defer q.observerMu.Unlock()
	q.syncActivityInternal(ctx, run)
	if len(q.pendingSync) > 0 {
		q.signalInternal()
	}
}

func (q *Queue) syncActivityInternal(ctx context.Context, run scheduler.Run) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	key := queuePrefixInternal + kit.SHA256Hex(run.EnvironmentID+"\x00"+run.JobID) + "/" + run.ID
	if err := q.observer.SyncRunActivity(ctx, run); err != nil {
		if q.pendingSync == nil {
			q.pendingSync = make(map[string]scheduler.Run)
		}
		q.pendingSync[key] = run
		slog.ErrorContext(ctx, "Job activity synchronization failed; will retry", "runId", run.ID, "error", err)
		return
	}
	delete(q.pendingSync, key)
}

func (q *Queue) retryActivitySyncInternal(ctx context.Context) {
	q.observerMu.Lock()
	defer q.observerMu.Unlock()
	for _, run := range q.pendingSync {
		if ctx.Err() != nil {
			return
		}
		q.syncActivityInternal(ctx, run)
	}
}

func (q *Queue) hasPendingActivitySyncInternal() bool {
	q.observerMu.Lock()
	defer q.observerMu.Unlock()
	return len(q.pendingSync) > 0
}
