package actors

import (
	"context"
	"sync"

	"emperror.dev/errors"
	"github.com/anthdm/hollywood/actor"
)

var errActorRequestNoResponseInternal = errors.Sentinel("actor request completed without response")

// Context exposes the operations available while handling one actor message
// without leaking Hollywood into actor consumers.
type Context struct {
	context context.Context
	engine  *actor.Engine
	target  *actor.PID
	respond func(any)
	workers *sync.WaitGroup
}

// Context returns the current receiver lifetime.
func (c *Context) Context() context.Context {
	return c.context
}

// Respond sends exactly one response to the current request.
func (c *Context) Respond(message any) {
	c.respond(message)
}

// Behavior defines the application behavior hosted by one shared actor.
type Behavior struct {
	Initialize func(*Context)
	Handle     func(*Context, any)
	Cleanup    func(*Context)
}

// Binding connects an ingress to an actor for the duration of its lifetime.
type Binding interface {
	bindInternal(handle *handleInternal)
	unbindInternal(handle *handleInternal)
	replayInternal()
}

// Actor owns one Hollywood process, its ingress bindings, and all workers
// started from its behavior.
type Actor struct {
	handle     *handleInternal
	bindings   []Binding
	unbindOnce sync.Once
}

type behaviorActorInternal struct {
	behavior Behavior
	bindings []Binding
	lifetime lifetimeInternal
	workers  sync.WaitGroup
}

// NewActor starts one application behavior on the shared actor runtime.
func NewActor(ctx context.Context, runtime *Runtime, kind, id string, maxRestarts int32, producer func() Behavior, bindings ...Binding) (*Actor, error) {
	if ctx == nil {
		return nil, errors.New("actor context unavailable")
	}
	if runtime == nil {
		return nil, errors.New("actor runtime unavailable")
	}
	if producer == nil {
		return nil, errors.New("actor behavior unavailable")
	}

	handle, err := runtime.startInternal(
		func() actor.Receiver {
			return &behaviorActorInternal{behavior: producer(), bindings: bindings}
		},
		kind,
		id,
		maxRestarts,
		actor.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}

	applicationActor := &Actor{handle: handle, bindings: bindings}
	for _, binding := range bindings {
		binding.bindInternal(handle)
	}
	return applicationActor, nil
}

// Send queues a message for the actor.
func (a *Actor) Send(message any) error {
	return a.handle.Send(message)
}

// Done closes after the actor and its workers stop.
func (a *Actor) Done() <-chan struct{} {
	return a.handle.Done()
}

// Stop disconnects ingress, drains the mailbox, cancels workers, and joins
// them before the actor terminates.
func (a *Actor) Stop(ctx context.Context) error {
	a.unbindOnce.Do(func() {
		for _, binding := range a.bindings {
			binding.unbindInternal(a.handle)
		}
	})
	return a.handle.Stop(ctx)
}

// Request sends one typed request and consumes its response.
func (a *Actor) Request[K comparable, Q, R any](ctx context.Context, request Message[K, Q]) (Message[K, R], error) {
	if a == nil {
		var zero Message[K, R]
		return zero, errors.New("actor unavailable")
	}
	return a.handle.request[K, Q, R](ctx, request)
}

func (a *behaviorActorInternal) Receive(ctx *actor.Context) {
	switch message := ctx.Message().(type) {
	case actor.Initialized:
		return
	case actor.Started:
		lifetimeCtx := a.lifetime.startInternal(ctx.Context())
		for _, binding := range a.bindings {
			binding.replayInternal()
		}
		if a.behavior.Initialize != nil {
			a.behavior.Initialize(a.contextInternal(lifetimeCtx, ctx))
		}
	case actor.Stopped:
		a.lifetime.stopInternal()
		if a.behavior.Cleanup != nil {
			a.behavior.Cleanup(a.contextInternal(ctx.Context(), ctx))
		}
		// Strict joining preserves actor serialization across receiver restarts:
		// a replacement must never run while an old worker can still own state.
		// Behavior workers are therefore required to honor their lifetime context.
		a.workers.Wait()
	default:
		if request, ok := message.(requestInternal); ok {
			defer request.reply.Resolve(requestResultInternal{err: errActorRequestNoResponseInternal})
			if a.behavior.Handle != nil {
				requestCtx := a.contextInternal(a.lifetime.contextInternal(), ctx)
				requestCtx.respond = func(message any) {
					request.reply.Resolve(requestResultInternal{message: message})
				}
				a.behavior.Handle(requestCtx, request.message)
			}
			return
		}
		if a.behavior.Handle != nil {
			a.behavior.Handle(a.contextInternal(a.lifetime.contextInternal(), ctx), message)
		}
	}
}

func (a *behaviorActorInternal) contextInternal(lifetimeCtx context.Context, ctx *actor.Context) *Context {
	return &Context{
		context: lifetimeCtx,
		engine:  ctx.Engine(),
		target:  ctx.PID().CloneVT(),
		respond: ctx.Respond,
		workers: &a.workers,
	}
}
