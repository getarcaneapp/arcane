package actors

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type actorBindingFakeInternal struct {
	binds   atomic.Int32
	unbinds atomic.Int32
}

func (b *actorBindingFakeInternal) bindInternal(*handleInternal) {
	b.binds.Add(1)
}

func (b *actorBindingFakeInternal) unbindInternal(*handleInternal) {
	b.unbinds.Add(1)
}

func (b *actorBindingFakeInternal) replayInternal() {}

func TestActorOwnsLifecycleBindingsAndWorkerJoinInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)

	binding := &actorBindingFakeInternal{}
	initialized := make(chan struct{})
	workerExited := make(chan struct{})
	applicationActor, err := NewActor(
		t.Context(),
		runtime,
		"actor-test",
		"lifecycle",
		1,
		func() Behavior {
			return Behavior{
				Initialize: func(ctx *Context) {
					close(initialized)
					Worker[uint8, NoPayload]{
						Actor:          ctx,
						WorkContext:    ctx.Context(),
						Label:          "actor lifecycle test worker",
						CompletionKind: 1,
					}.Start(func(workerCtx context.Context) (NoPayload, error) {
						defer close(workerExited)
						<-workerCtx.Done()
						return NoPayload{}, workerCtx.Err()
					})
				},
			}
		},
		binding,
	)
	require.NoError(t, err)
	require.Equal(t, int32(1), binding.binds.Load())
	<-initialized

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, applicationActor.Stop(stopCtx))
	<-workerExited
	require.Equal(t, int32(1), binding.unbinds.Load())
	require.NoError(t, lifecycle.Stop(stopCtx))
}
