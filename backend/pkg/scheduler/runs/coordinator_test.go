package runs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
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
	q := newTestCoordinator(t)
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
