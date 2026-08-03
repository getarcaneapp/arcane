package actors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestRunnerCancelsAndJoinsBackgroundFunctionInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	started := make(chan struct{})
	exited := make(chan struct{})
	runner, err := NewRunner(t.Context(), runtime, "runner-test", "lifecycle", "test runner", 1, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(exited)
		return nil
	})
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(time.Second):
		require.FailNow(t, "runner did not start")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runner.Stop(stopCtx))
	select {
	case <-exited:
	default:
		require.FailNow(t, "runner stop returned before background function exited")
	}
	require.NoError(t, lifecycle.Stop(stopCtx))
}
