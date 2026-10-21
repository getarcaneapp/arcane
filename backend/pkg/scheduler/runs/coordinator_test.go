package runs

import (
	"context"
	"sync"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

type claimingObserverFake struct {
	once    sync.Once
	target  string
	claim   func(types.SchedulerRun)
	claimed bool
}

func (o *claimingObserverFake) ActivityID(run types.SchedulerRun) string {
	if o.target != "" && run.ID == o.target && run.Status == types.RunStatusQueued {
		o.once.Do(func() {
			o.claimed = true
			o.claim(run)
		})
	}
	return ""
}

func (o *claimingObserverFake) SyncRunActivity(context.Context, types.SchedulerRun) error { return nil }

func TestSubmitRetryAfterClaimAppendsFreshRun(t *testing.T) {
	q := newResolutionQueue(t)
	observer := &claimingObserverFake{}
	q.SetObserver(observer)
	queued, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "coalesce", Trigger: "scheduled"})
	require.NoError(t, err)
	observer.target = queued.ID
	observer.claim = func(run types.SchedulerRun) {
		claim, claimErr := q.claimExecution(t.Context(), types.SchedulerExecutionCommand{
			EnvironmentID: run.EnvironmentID,
			JobID:         run.JobID,
			RunID:         run.ID,
			Attempt:       1,
		})
		require.NoError(t, claimErr)
		require.True(t, claim.Claimed)
	}

	submitted, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "coalesce", Trigger: "scheduled"})
	require.NoError(t, err)
	require.True(t, observer.claimed)
	require.NotEqual(t, queued.ID, submitted.ID)
	require.Equal(t, types.RunStatusQueued, submitted.Status)

	history, err := q.List(t.Context(), "0", "coalesce", 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, history.Total)
	statuses := map[string]types.RunStatus{}
	for _, run := range history.Runs {
		statuses[run.ID] = run.Status
	}
	require.Equal(t, map[string]types.RunStatus{queued.ID: types.RunStatusRunning, submitted.ID: types.RunStatusQueued}, statuses)
}
