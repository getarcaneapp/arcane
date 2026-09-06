package scheduler

import (
	"context"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func newTestAdmissionGateInternal(t *testing.T) *actors.Gate[actors.AdmissionKey] {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "scheduler-test-admission", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return gate
}

func newJobSchedulerForTestInternal(t testing.TB, ctx context.Context, location *time.Location) *jobSchedulerInternal {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	created, err := NewJobScheduler(ctx, runtime, location)
	require.NoError(t, err)
	scheduler := created.(*jobSchedulerInternal)
	scheduler.SetDispatcher(&testDispatcherInternal{run: func(ctx context.Context, request schedulertypes.Request) error {
		job, ok := scheduler.GetJob(request.JobID)
		if !ok {
			return nil
		}
		_, err := job.Run(ctx)
		return err
	}})
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, scheduler.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return scheduler
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(t.Context(), runtime, "settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, executor.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, executor, effects)
}

type testDispatcherInternal struct {
	run   func(context.Context, schedulertypes.Request) error
	locks sync.Map
}

func (d *testDispatcherInternal) Submit(ctx context.Context, request schedulertypes.Request) (schedulertypes.Run, error) {
	value, _ := d.locks.LoadOrStore(request.JobID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return schedulertypes.Run{}, nil
	}
	defer lock.Unlock()
	return schedulertypes.Run{JobID: request.JobID}, d.run(ctx, request)
}
func (*testDispatcherInternal) Checkpoint(context.Context, string, string, time.Time) error {
	return nil
}
