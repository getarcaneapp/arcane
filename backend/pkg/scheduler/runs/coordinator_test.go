package runs

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/actor"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	francistest "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis/testing"
)

type (
	claimingObserverFake struct {
		claim   func(types.SchedulerRun)
		target  string
		once    sync.Once
		claimed bool
	}
)

func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runtime := francistest.New(t)
	q := New(kv.NewKVService(&database.DB{DB: db}), runtime.Service(), time.UTC)
	require.NoError(t, q.Register(runtime))
	francistest.Start(t, runtime)
	return q
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
	q := newTestCoordinator(t)
	observer := &claimingObserverFake{}
	q.SetObserver(observer)
	queued, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "coalesce", Trigger: "scheduled"})
	require.NoError(t, err)
	observer.target = queued.ID
	observer.claim = func(run types.SchedulerRun) {
		page, listErr := q.service.ListStates(t.Context(), coordinatorType, &actor.ListStatesOpts{IncludeData: true})
		require.NoError(t, listErr)
		require.Len(t, page.States, 1)
		var state types.SchedulerCoordinatorState
		require.NoError(t, page.States[0].Data.Decode(&state))
		for i := range state.Record.Runs {
			if state.Record.Runs[i].ID == run.ID {
				state.Record.Runs[i].Status = types.RunStatusRunning
			}
		}
		state.Revision++
		require.NoError(t, q.service.SetState(t.Context(), coordinatorType, page.States[0].ActorID, state, nil))
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

func TestConcurrentUpdateRunOnOneJobAllSucceed(t *testing.T) {
	q := newTestCoordinator(t)
	run, err := q.Submit(t.Context(), types.SchedulerRequest{JobID: "concurrent", Trigger: "manual"})
	require.NoError(t, err)
	const writers = 25
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			errs <- q.UpdateRun(t.Context(), run, func(current *types.SchedulerRun) error {
				current.Outcome.Targets = append(current.Outcome.Targets, types.TargetOutcome{ID: fmt.Sprintf("target-%d", i), Status: types.RunStatusSucceeded})
				return nil
			})
		})
	}
	wg.Wait()
	close(errs)
	for updateErr := range errs {
		require.NoError(t, updateErr)
	}
	stored, err := q.Get(t.Context(), "0", run.JobID, run.ID)
	require.NoError(t, err)
	require.Len(t, stored.Outcome.Targets, writers)
}
