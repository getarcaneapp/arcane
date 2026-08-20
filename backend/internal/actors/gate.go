package actors

import (
	"context"
	"sync"

	"emperror.dev/errors"
)

type gateOperationInternal uint8

const (
	gateAcquireInternal gateOperationInternal = iota
	gateReleaseInternal
)

// AdmissionKey namespaces a string identifier shared by the application gate.
type AdmissionKey struct {
	Scope string
	ID    string
}

// Gate admits at most one active lease for each key through one actor-owned map.
type Gate[K comparable] struct {
	actor *Actor
}

// Lease represents one admitted key. Release is idempotent and confirms that
// the actor processed the completion before returning.
type Lease[K comparable] struct {
	gate *Gate[K]
	key  K
	once sync.Once
}

type gateCommandInternal[K comparable] struct {
	key   K
	reply *Promise[bool]
}

type gateActorInternal[K comparable] struct {
	active map[K]struct{}
}

// NewGate creates a keyed admission gate on the shared actor runtime.
func NewGate[K comparable](ctx context.Context, runtime *Runtime, kind, id string) (*Gate[K], error) {
	if ctx == nil {
		return nil, errors.New("gate context unavailable")
	}
	if runtime == nil {
		return nil, errors.New("actor runtime unavailable")
	}

	active := make(map[K]struct{})
	applicationActor, err := NewActor(
		ctx,
		runtime,
		kind,
		id,
		// Shared gates tolerate transient receiver failures without forgetting
		// active leases, whose map intentionally survives receiver replacement.
		3,
		func() Behavior {
			// The map intentionally survives receiver replacement so an actor restart
			// cannot forget leases held by work that is still running.
			state := &gateActorInternal[K]{active: active}
			return Behavior{Handle: state.receiveInternal}
		},
	)
	if err != nil {
		return nil, err
	}
	return &Gate[K]{actor: applicationActor}, nil
}

// TryAcquire returns a lease when key is idle and refuses immediately when it
// is already active. Actor failure is reported separately from contention.
func (g *Gate[K]) TryAcquire(ctx context.Context, key K) (*Lease[K], bool, error) {
	if ctx == nil {
		return nil, false, errors.New("gate request context unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if g == nil || g.actor == nil {
		return nil, false, errors.New("actor gate unavailable")
	}

	reply := NewPromise[bool]()
	if err := g.actor.Send(Message[gateOperationInternal, gateCommandInternal[K]]{
		Kind: gateAcquireInternal,
		Value: gateCommandInternal[K]{
			key:   key,
			reply: reply,
		},
	}); err != nil {
		return nil, false, err
	}

	// Admission must resolve after it is queued; abandoning an admitted reply on
	// caller cancellation would leak the key without a lease to release it.
	admitted, err := g.actor.handle.await(context.WithoutCancel(ctx), reply, errors.New("actor gate stopped"))
	if err != nil {
		return nil, false, err
	}
	if !admitted {
		return nil, false, nil
	}
	return &Lease[K]{gate: g, key: key}, true, nil
}

// Release returns the lease to its gate exactly once.
func (l *Lease[K]) Release() {
	if l == nil || l.gate == nil {
		return
	}
	l.once.Do(func() {
		reply := NewPromise[bool]()
		if err := l.gate.actor.Send(Message[gateOperationInternal, gateCommandInternal[K]]{
			Kind: gateReleaseInternal,
			Value: gateCommandInternal[K]{
				key:   l.key,
				reply: reply,
			},
		}); err != nil {
			return
		}
		_, _ = l.gate.actor.handle.await(context.Background(), reply, errors.New("actor gate stopped"))
	})
}

// Stop drains the gate mailbox and waits for the actor to terminate.
func (g *Gate[K]) Stop(ctx context.Context) error {
	if g == nil || g.actor == nil {
		return nil
	}
	return g.actor.Stop(ctx)
}

func (g *gateActorInternal[K]) receiveInternal(_ *Context, rawMessage any) {
	message, ok := rawMessage.(Message[gateOperationInternal, gateCommandInternal[K]])
	if !ok || message.Value.reply == nil {
		return
	}
	// Resolve a refusal if future gate logic panics before producing a result.
	// Promise resolution is idempotent, so successful paths win first.
	defer message.Value.reply.Resolve(false)

	switch message.Kind {
	case gateAcquireInternal:
		if _, active := g.active[message.Value.key]; active {
			message.Value.reply.Resolve(false)
			return
		}
		g.active[message.Value.key] = struct{}{}
		message.Value.reply.Resolve(true)
	case gateReleaseInternal:
		delete(g.active, message.Value.key)
		message.Value.reply.Resolve(true)
	}
}
