package actors

import (
	"context"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type panicActorInternal struct{}

func (panicActorInternal) Receive(ctx *actor.Context) {
	if _, ok := ctx.Message().(panicActorMessageInternal); ok {
		panic("deliberate actor test panic")
	}
}

type panicActorMessageInternal struct{}

type stopBlockingActorInternal struct {
	release <-chan struct{}
}

func (a stopBlockingActorInternal) Receive(ctx *actor.Context) {
	if _, ok := ctx.Message().(actor.Stopped); ok {
		<-a.release
	}
}

func TestRuntimeMonitorObservesRestartsMaxRestartsAndDeadLettersInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	handle, err := runtime.startInternal(
		func() actor.Receiver { return panicActorInternal{} },
		"panic-test",
		"restart-limit",
		3,
		actor.WithContext(t.Context()),
		actor.WithRestartDelay(0),
	)
	require.NoError(t, err)
	pid := handle.pid

	for expectedRestarts := uint64(1); expectedRestarts <= 3; expectedRestarts++ {
		require.NoError(t, handle.Send(panicActorMessageInternal{}))
		require.Eventually(t, func() bool {
			return runtime.RestartCount() == expectedRestarts
		}, time.Second, time.Millisecond)
	}

	require.NoError(t, handle.Send(panicActorMessageInternal{}))
	require.Eventually(t, func() bool {
		return runtime.MaxRestartsExceededCount() == 1
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-handle.Done():
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	runtime.engine.Send(pid, panicActorMessageInternal{})
	require.Eventually(t, func() bool {
		return runtime.DeadLetterCount() == 1
	}, time.Second, time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestHandleCanceledStopStillTerminatesAndCanBeJoinedInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	release := make(chan struct{})
	handle, err := runtime.startInternal(
		func() actor.Receiver { return stopBlockingActorInternal{release: release} },
		"handle-test",
		"stop",
		1,
		actor.WithContext(t.Context()),
	)
	require.NoError(t, err)
	require.NotNil(t, handle)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, handle.Stop(canceled), context.Canceled)
	close(release)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, handle.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestRuntimeRejectsDuplicateActorIDWithoutOverwritingOwnerInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	first, err := runtime.startInternal(
		func() actor.Receiver { return panicActorInternal{} },
		"duplicate-test",
		"same-id",
		1,
		actor.WithContext(t.Context()),
	)
	require.NoError(t, err)

	second, err := runtime.startInternal(
		func() actor.Receiver { return panicActorInternal{} },
		"duplicate-test",
		"same-id",
		1,
		actor.WithContext(t.Context()),
	)
	require.Nil(t, second)
	require.ErrorContains(t, err, "actor ID already in use")
	require.Equal(t, actor.NewPID(runtime.engine.Address(), "duplicate-test/same-id").String(), first.pid.String())

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, first.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestRuntimeMonitorResubscribesOnStartedInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	runtime.engine.Unsubscribe(runtime.monitorPID)
	time.Sleep(10 * time.Millisecond)
	runtime.engine.Send(runtime.monitorPID, actor.Started{})
	time.Sleep(10 * time.Millisecond)
	runtime.engine.Send(actor.NewPID(runtime.engine.Address(), "missing/actor"), "dead letter")
	require.Eventually(t, func() bool {
		return runtime.DeadLetterCount() == 1
	}, time.Second, time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}
