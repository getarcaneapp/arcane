package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"emperror.dev/errors"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

type activityObserverFakeInternal struct {
	unavailable atomic.Bool
	observed    atomic.Int32
}

func (o *activityObserverFakeInternal) ActivityID(run st.Run) string { return run.ID }

func (o *activityObserverFakeInternal) SyncRunActivity(context.Context, st.Run) error {
	if o.unavailable.Load() {
		return errors.New("activity storage unavailable")
	}
	o.observed.Add(1)
	return nil
}

func TestActivityFailureNeverReplaysAcceptedExecution(t *testing.T) {
	q := newResolutionQueueInternal(t)
	observer := &activityObserverFakeInternal{}
	observer.unavailable.Store(true)
	q.SetObserver(observer)
	var executions atomic.Int32
	q.execute = func(context.Context, st.Run) (st.Outcome, error) {
		executions.Add(1)
		return st.Outcome{Status: st.Succeeded}, nil
	}
	run, err := q.Submit(t.Context(), st.Request{JobID: "test-job", Trigger: "manual"})
	require.NoError(t, err)
	require.Equal(t, run.ID, run.ActivityID)
	require.NoError(t, q.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, q.Stop(ctx))
	})
	require.Eventually(t, func() bool {
		persisted, getErr := q.Get(t.Context(), "0", run.JobID, run.ID)
		return getErr == nil && persisted.Status == st.Succeeded
	}, time.Second, time.Millisecond)
	require.True(t, q.hasPendingActivitySyncInternal())
	observer.unavailable.Store(false)
	q.retryActivitySyncInternal(t.Context())
	require.False(t, q.hasPendingActivitySyncInternal())
	require.Positive(t, observer.observed.Load())
	duplicate, err := q.Submit(t.Context(), st.Request{JobID: run.JobID, RunID: run.ID, Trigger: "manual"})
	require.NoError(t, err)
	require.Equal(t, st.Succeeded, duplicate.Status)
	require.Equal(t, int32(1), executions.Load())
}
