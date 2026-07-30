package actors

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestResourceSerializesRestartWorkAndStopInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	var (
		mu      sync.Mutex
		stopped []int
	)
	resource, err := NewResource(t.Context(), runtime, "resource-test", "lifecycle", 1, func(value int) error {
		mu.Lock()
		stopped = append(stopped, value)
		mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, resource.Restart(t.Context(), "start first", func(context.Context) (int, error) { return 1, nil }))
	require.NoError(t, resource.Restart(t.Context(), "start second", func(context.Context) (int, error) { return 2, nil }))

	var current int
	require.NoError(t, resource.Do(t.Context(), "read current", func(_ context.Context, value int) error {
		current = value
		return nil
	}))
	require.Equal(t, 2, current)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, resource.Stop(stopCtx))
	mu.Lock()
	require.Equal(t, []int{1, 2}, stopped)
	mu.Unlock()
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestResourceFailedReplacementCleansPartialValueInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	var stopped []int
	resource, err := NewResource(t.Context(), runtime, "resource-test", "failed-replacement", 1, func(value int) error {
		stopped = append(stopped, value)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, resource.Restart(t.Context(), "start", func(context.Context) (int, error) { return 1, nil }))
	require.Error(t, resource.Restart(t.Context(), "replace", func(context.Context) (int, error) {
		return 2, errors.New("build failed")
	}))
	require.Equal(t, []int{1, 2}, stopped)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, resource.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestResourceCanceledStopStillCleansAndFencesInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	stopped := make(chan struct{})
	resource, err := NewResource(t.Context(), runtime, "resource-test", "canceled-stop", 1, func(int) error {
		close(stopped)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, resource.Restart(t.Context(), "start", func(context.Context) (int, error) { return 1, nil }))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, resource.Stop(canceled), context.Canceled)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("resource cleanup was abandoned after stop wait cancellation")
	}
	require.ErrorIs(t, resource.Restart(t.Context(), "restart", func(context.Context) (int, error) { return 2, nil }), ErrResourceStopped)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, resource.executor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
