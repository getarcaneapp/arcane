package runs

import (
	"context"
	"sync"
	"testing"

	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

type claimingObserverFake struct {
	once    sync.Once
	target  string
	claim   func(st.Run)
	claimed bool
}

func (o *claimingObserverFake) ActivityID(run st.Run) string {
	if o.target != "" && run.ID == o.target && run.Status == st.Queued {
		o.once.Do(func() {
			o.claimed = true
			o.claim(run)
		})
	}
	return ""
}

func (o *claimingObserverFake) SyncRunActivity(context.Context, st.Run) error { return nil }

func TestSubmitRetryAfterClaimAppendsFreshRun(t *testing.T) {
	q := newResolutionQueue(t)
	observer := &claimingObserverFake{}
	q.SetObserver(observer)
	queued, err := q.Submit(t.Context(), st.Request{JobID: "coalesce", Trigger: "scheduled"})
	require.NoError(t, err)
	observer.target = queued.ID
	observer.claim = func(run st.Run) {
		claim, claimErr := q.claimExecution(t.Context(), st.ExecutionCommand{
			EnvironmentID: run.EnvironmentID,
			JobID:         run.JobID,
			RunID:         run.ID,
			Attempt:       1,
		})
		require.NoError(t, claimErr)
		require.True(t, claim.Claimed)
	}

	submitted, err := q.Submit(t.Context(), st.Request{JobID: "coalesce", Trigger: "scheduled"})
	require.NoError(t, err)
	require.True(t, observer.claimed)
	require.NotEqual(t, queued.ID, submitted.ID)
	require.Equal(t, st.Queued, submitted.Status)

	history, err := q.List(t.Context(), "0", "coalesce", 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, history.Total)
	statuses := map[string]st.RunStatus{}
	for _, run := range history.Runs {
		statuses[run.ID] = run.Status
	}
	require.Equal(t, map[string]st.RunStatus{queued.ID: st.Running, submitted.ID: st.Queued}, statuses)
}
