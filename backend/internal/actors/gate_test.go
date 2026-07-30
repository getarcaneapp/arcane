package actors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestGateSerializesEachKeyAndReleasesIndependentlyInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	gate, err := NewGate[string](t.Context(), runtime, "gate-test", "keyed")
	require.NoError(t, err)

	first, admitted, err := gate.TryAcquire(t.Context(), "first")
	require.NoError(t, err)
	require.True(t, admitted)

	_, admitted, err = gate.TryAcquire(t.Context(), "first")
	require.NoError(t, err)
	require.False(t, admitted)

	second, admitted, err := gate.TryAcquire(t.Context(), "second")
	require.NoError(t, err)
	require.True(t, admitted)
	second.Release()

	first.Release()
	first.Release()
	replacement, admitted, err := gate.TryAcquire(t.Context(), "first")
	require.NoError(t, err)
	require.True(t, admitted)
	replacement.Release()

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, gate.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestGateReturnsContextCancellationWithoutLeakingKeyInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	gate, err := NewGate[string](t.Context(), runtime, "gate-test", "cancellation")
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, admitted, err := gate.TryAcquire(canceled, "key")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, admitted)

	lease, admitted, err := gate.TryAcquire(t.Context(), "key")
	require.NoError(t, err)
	require.True(t, admitted)
	lease.Release()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, gate.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
