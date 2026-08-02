package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
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
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, scheduler.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return scheduler
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*services.SettingsService, error) {
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
	return services.NewSettingsService(ctx, db, executor, effects)
}
