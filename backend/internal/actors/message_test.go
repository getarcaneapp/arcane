package actors

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type messageKindInternal uint8

const (
	ingressMessageInternal messageKindInternal = iota
	timerStartMessageInternal
	timerElapsedMessageInternal
	requestMessageInternal
	responseMessageInternal
	workerCompletedMessageInternal
)

type ingressReceiverInternal struct {
	ingress *Ingress[messageKindInternal, int]
	values  chan<- int
}

func (r *ingressReceiverInternal) Receive(ctx *actor.Context) {
	if message, ok := ctx.Message().(Message[messageKindInternal, NoPayload]); ok && message.Kind == ingressMessageInternal {
		r.values <- r.ingress.Take()
	}
}

func TestIngressCoalescesBeforeBindingAndRetainsLatestValueInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	ingress := NewIngress[messageKindInternal, int](ingressMessageInternal)
	values := make(chan int, 2)
	pid := engine.Spawn(
		func() actor.Receiver { return &ingressReceiverInternal{ingress: ingress, values: values} },
		"ingress-test",
		actor.WithID("coalesced"),
		actor.WithMaxRestarts(1),
	)

	ingress.Send(1)
	ingress.Send(2)
	handle := &handleInternal{engine: engine, pid: pid, done: make(chan struct{})}
	ingress.bindInternal(handle)
	require.Equal(t, 2, <-values)

	ingress.Send(3)
	require.Equal(t, 3, <-values)
	ingress.unbindInternal(handle)
	<-engine.Poison(pid).Done()
}

type timerReceiverInternal struct {
	timer   Timer[messageKindInternal]
	elapsed chan<- uint64
}

func (r *timerReceiverInternal) Receive(ctx *actor.Context) {
	switch message := ctx.Message().(type) {
	case Message[messageKindInternal, NoPayload]:
		if message.Kind == timerStartMessageInternal {
			actorCtx := &Context{
				context: ctx.Context(),
				engine:  ctx.Engine(),
				target:  ctx.PID().CloneVT(),
				respond: ctx.Respond,
				workers: &sync.WaitGroup{},
			}
			r.timer.Reset(actorCtx, timerElapsedMessageInternal, time.Hour)
			r.timer.Reset(actorCtx, timerElapsedMessageInternal, time.Millisecond)
		}
	case Message[messageKindInternal, uint64]:
		if message.Kind == timerElapsedMessageInternal && r.timer.Current(message.Value) {
			r.elapsed <- message.Value
		}
	case actor.Stopped:
		r.timer.Stop()
	}
}

func TestTimerOnlyDeliversLatestGenerationInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	elapsed := make(chan uint64, 2)
	pid := engine.Spawn(
		func() actor.Receiver { return &timerReceiverInternal{elapsed: elapsed} },
		"timer-test",
		actor.WithID("latest"),
		actor.WithMaxRestarts(1),
	)
	engine.Send(pid, Message[messageKindInternal, NoPayload]{Kind: timerStartMessageInternal})
	require.Equal(t, uint64(2), <-elapsed)
	require.Never(t, func() bool { return len(elapsed) > 0 }, 20*time.Millisecond, time.Millisecond)
	<-engine.Poison(pid).Done()
}

func TestRequestReturnsTypedResponseInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	applicationActor, err := NewActor(
		t.Context(),
		runtime,
		"request-test",
		"typed",
		1,
		func() Behavior {
			return Behavior{Handle: func(ctx *Context, rawMessage any) {
				if message, ok := rawMessage.(Message[messageKindInternal, int]); ok && message.Kind == requestMessageInternal {
					ctx.Respond(Message[messageKindInternal, string]{Kind: responseMessageInternal, Value: "ok"})
				}
			}}
		},
	)
	require.NoError(t, err)
	for range 10 {
		response, requestErr := applicationActor.Request[messageKindInternal, int, string](t.Context(), Message[messageKindInternal, int]{Kind: requestMessageInternal, Value: 1})
		require.NoError(t, requestErr)
		require.Equal(t, responseMessageInternal, response.Kind)
		require.Equal(t, "ok", response.Value)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, applicationActor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestRequestFailsWhenHandlerReturnsWithoutResponseInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	applicationActor, err := NewActor(
		t.Context(),
		runtime,
		"request-test",
		"missing-response",
		1,
		func() Behavior {
			return Behavior{Handle: func(*Context, any) {}}
		},
	)
	require.NoError(t, err)

	_, err = applicationActor.Request[messageKindInternal, int, string](context.Background(), Message[messageKindInternal, int]{Kind: requestMessageInternal})
	require.ErrorContains(t, err, "without response")

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, applicationActor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

type workerReceiverInternal struct {
	errors chan<- error
	values chan<- string
}

func (r workerReceiverInternal) Receive(ctx *actor.Context) {
	if message, ok := ctx.Message().(Message[messageKindInternal, string]); ok && message.Kind == workerCompletedMessageInternal {
		defer message.Acknowledge()
		if r.errors != nil {
			r.errors <- message.Err
		}
		if r.values != nil {
			r.values <- message.Value
		}
	}
}

func TestWorkerContainsPanicAndDeliversCompletionInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	workerErrors := make(chan error, 1)
	workerValues := make(chan string, 1)
	pid := engine.Spawn(
		func() actor.Receiver { return workerReceiverInternal{errors: workerErrors, values: workerValues} },
		"worker-test",
		actor.WithID("panic"),
		actor.WithMaxRestarts(1),
	)

	var wg sync.WaitGroup
	Worker[messageKindInternal, string]{
		Actor: &Context{
			context: context.Background(),
			engine:  engine,
			target:  pid,
			workers: &wg,
		},
		WorkContext:    context.Background(),
		Label:          "deliberate worker test",
		CompletionKind: workerCompletedMessageInternal,
		PanicValue:     "panic fallback",
	}.Start(func(context.Context) (string, error) {
		panic("deliberate worker panic")
	})

	require.ErrorContains(t, <-workerErrors, "deliberate worker test panicked")
	require.Equal(t, "panic fallback", <-workerValues)
	wg.Wait()
	<-engine.Poison(pid).Done()
}

func TestWorkerRetriesContainedPanicInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	workerErrors := make(chan error, 1)
	workerValues := make(chan string, 1)
	pid := engine.Spawn(
		func() actor.Receiver { return workerReceiverInternal{errors: workerErrors, values: workerValues} },
		"worker-test",
		actor.WithID("retry"),
		actor.WithMaxRestarts(1),
	)

	var (
		wg       sync.WaitGroup
		attempts int
	)
	Worker[messageKindInternal, string]{
		Actor: &Context{
			context: context.Background(),
			engine:  engine,
			target:  pid,
			workers: &wg,
		},
		WorkContext:    context.Background(),
		Label:          "retrying worker test",
		CompletionKind: workerCompletedMessageInternal,
		PanicValue:     "panic fallback",
		RetryDelay:     time.Millisecond,
	}.Start(func(context.Context) (string, error) {
		attempts++
		if attempts == 1 {
			panic("deliberate retryable panic")
		}
		return "completed", nil
	})

	require.NoError(t, <-workerErrors)
	require.Equal(t, "completed", <-workerValues)
	require.Equal(t, 2, attempts)
	wg.Wait()
	<-engine.Poison(pid).Done()
}

type lifetimeReceiverInternal struct {
	lifetime lifetimeInternal
	started  chan<- context.Context
}

func (r *lifetimeReceiverInternal) Receive(ctx *actor.Context) {
	switch ctx.Message().(type) {
	case actor.Started:
		r.started <- r.lifetime.startInternal(ctx.Context())
	case panicActorMessageInternal:
		panic("deliberate lifetime restart")
	case actor.Stopped:
		r.lifetime.stopInternal()
	}
}

func TestLifetimeCancelsOldReceiverBeforeHollywoodRestartInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	started := make(chan context.Context, 2)
	pid := engine.Spawn(
		func() actor.Receiver { return &lifetimeReceiverInternal{started: started} },
		"lifetime-test",
		actor.WithID("restart"),
		actor.WithMaxRestarts(1),
		actor.WithRestartDelay(0),
	)

	first := <-started
	engine.Send(pid, panicActorMessageInternal{})
	require.Eventually(t, func() bool { return first.Err() != nil }, time.Second, time.Millisecond)
	second := <-started
	require.NoError(t, second.Err())
	<-engine.Poison(pid).Done()
}

func TestWorkerResolvesFallbackWhenActorStopsBeforeCompletionInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	pid := engine.Spawn(
		func() actor.Receiver { return workerReceiverInternal{errors: make(chan error, 1)} },
		"worker-test",
		actor.WithID("stopped"),
		actor.WithMaxRestarts(1),
	)

	actorCtx, stopActor := context.WithCancel(context.Background())
	result := NewPromise[error]()
	var wg sync.WaitGroup
	Worker[messageKindInternal, string]{
		Actor: &Context{
			context: actorCtx,
			engine:  engine,
			target:  pid,
			workers: &wg,
		},
		WorkContext:    context.Background(),
		Label:          "stopped worker test",
		CompletionKind: workerCompletedMessageInternal,
		ActorStopped: func(_ string, err error) {
			result.Resolve(err)
		},
	}.Start(func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	stopActor()
	require.ErrorIs(t, <-result.Done(), context.Canceled)
	wg.Wait()
	<-engine.Poison(pid).Done()
}

type noAcknowledgementReceiverInternal struct {
	received chan<- struct{}
}

func (r noAcknowledgementReceiverInternal) Receive(ctx *actor.Context) {
	if message, ok := ctx.Message().(Message[messageKindInternal, string]); ok && message.Kind == workerCompletedMessageInternal {
		r.received <- struct{}{}
	}
}

func TestWorkerWithoutAcknowledgementUnblocksWhenActorLifetimeEndsInternal(t *testing.T) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	require.NoError(t, err)
	received := make(chan struct{}, 1)
	pid := engine.Spawn(
		func() actor.Receiver { return noAcknowledgementReceiverInternal{received: received} },
		"worker-test",
		actor.WithID("missing-ack"),
	)
	actorCtx, stopActor := context.WithCancel(context.Background())
	fallback := make(chan error, 1)
	var workers sync.WaitGroup
	Worker[messageKindInternal, string]{
		Actor: &Context{
			context: actorCtx,
			engine:  engine,
			target:  pid,
			workers: &workers,
		},
		WorkContext:    actorCtx,
		Label:          "missing acknowledgement worker",
		CompletionKind: workerCompletedMessageInternal,
		ActorStopped: func(_ string, err error) {
			fallback <- err
		},
	}.Start(func(context.Context) (string, error) {
		return "done", nil
	})

	<-received
	stopActor()
	require.NoError(t, <-fallback)
	require.NoError(t, utils.WaitGroup(t.Context(), &workers))
	<-engine.Poison(pid).Done()
}

func TestIngressReplaysUnacknowledgedValueAfterActorRestartInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	ingress := NewIngress[messageKindInternal, int](ingressMessageInternal)
	processed := make(chan int, 1)
	var attempts atomic.Int32
	applicationActor, err := NewActor(
		t.Context(),
		runtime,
		"ingress-test",
		"restart-replay",
		2,
		func() Behavior {
			return Behavior{Handle: func(_ *Context, rawMessage any) {
				message, ok := rawMessage.(Message[messageKindInternal, NoPayload])
				if !ok || message.Kind != ingressMessageInternal {
					return
				}
				value, generation := ingress.Begin()
				if attempts.Add(1) == 1 {
					panic("restart after ingress begin")
				}
				ingress.Acknowledge(generation)
				processed <- value
			}}
		},
		ingress,
	)
	require.NoError(t, err)

	ingress.Send(42)
	require.Equal(t, 42, <-processed)
	require.False(t, ingress.Pending())
	require.Equal(t, int32(2), attempts.Load())

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, applicationActor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
