package runs

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

type activityObserverFake struct {
	unavailable  atomic.Bool
	observed     atomic.Int32
	existingOnly bool
}

func (o *activityObserverFake) ActivityID(run types.SchedulerRun) string {
	if o.existingOnly {
		return run.ActivityID
	}
	return run.ID
}

func (o *activityObserverFake) SyncRunActivity(context.Context, types.SchedulerRun) error {
	if o.unavailable.Load() {
		return errors.New("activity storage unavailable")
	}
	o.observed.Add(1)
	return nil
}

func coordinatorStates(t *testing.T, q *Coordinator) []types.SchedulerCoordinatorState {
	t.Helper()
	states, err := q.states(t.Context())
	require.NoError(t, err)
	return states
}

func pendingActivitySync(t *testing.T, q *Coordinator) bool {
	t.Helper()
	return slices.ContainsFunc(coordinatorStates(t, q), func(state types.SchedulerCoordinatorState) bool { return len(state.PendingActivitySync) > 0 })
}

func TestActivityFailureNeverReplaysAcceptedExecution(t *testing.T) {
	q := newResolutionQueue(t)
	observer := &activityObserverFake{}
	observer.unavailable.Store(true)
	q.SetObserver(observer)
	var executions atomic.Int32
	q.execute = func(context.Context, types.SchedulerRun) (types.SchedulerOutcome, error) {
		executions.Add(1)
		return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
	}
	run, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "test-job", Trigger: "manual"})
	require.NoError(t, err)
	require.Equal(t, run.ID, run.ActivityID)
	require.NoError(t, q.Start(t.Context(), t.Context()))
	q.Activate()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		require.NoError(t, q.Stop(ctx))
	})
	require.Eventually(t, func() bool {
		persisted, getErr := q.Get(t.Context(), "0", run.JobID, run.ID)
		return getErr == nil && persisted.Status == types.RunStatusSucceeded
	}, 5*time.Second, 10*time.Millisecond)
	require.True(t, pendingActivitySync(t, q))
	observer.unavailable.Store(false)
	require.False(t, q.retryActivitySync(t.Context(), coordinatorStates(t, q)))
	require.False(t, pendingActivitySync(t, q))
	require.Positive(t, observer.observed.Load())
	duplicate, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: run.JobID, RunID: run.ID, Trigger: "manual"})
	require.NoError(t, err)
	require.Equal(t, types.RunStatusSucceeded, duplicate.Status)
	require.Equal(t, int32(1), executions.Load())
}
