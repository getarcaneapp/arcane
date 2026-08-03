package handlers

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
