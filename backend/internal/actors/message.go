package actors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// NoPayload is used by typed actor messages that only carry a kind.
type NoPayload struct{}

// Message is the shared typed envelope for actor commands, events, and replies.
// Worker completion handlers must call Acknowledge before returning.
type Message[K comparable, V any] struct {
	Kind  K
	Value V
	Err   error
	ack   chan struct{}
}

// Acknowledge confirms that an actor processed a worker completion.
func (m Message[K, V]) Acknowledge() {
	if m.ack == nil {
		return
	}
	select {
	case m.ack <- struct{}{}:
	default:
	}
}

// Promise is an idempotent one-result completion primitive.
type Promise[T any] struct {
	once sync.Once
	done chan T
}

// NewPromise creates a promise that accepts exactly one result.
func NewPromise[T any]() *Promise[T] {
	return &Promise[T]{done: make(chan T, 1)}
}

// Resolve publishes value once; later calls are harmless.
func (p *Promise[T]) Resolve(value T) {
	if p == nil {
		return
	}
	p.once.Do(func() { p.done <- value })
}

// Done returns the channel that receives the promised result.
func (p *Promise[T]) Done() <-chan T {
	if p == nil {
		return nil
	}
	return p.done
}

// await waits for a promise while preserving a result that races with caller
// cancellation or actor termination.
func (h *handleInternal) await[T any](ctx context.Context, promise *Promise[T], stoppedErr error) (T, error) {
	var zero T
	if ctx == nil || h == nil || promise == nil {
		return zero, errors.New("actor wait unavailable")
	}
	select {
	case result := <-promise.Done():
		return result, nil
	case <-ctx.Done():
		select {
		case result := <-promise.Done():
			return result, nil
		default:
			return zero, ctx.Err()
		}
	case <-h.Done():
		select {
		case result := <-promise.Done():
			return result, nil
		default:
			return zero, stoppedErr
		}
	}
}

// lifetimeInternal is an actor-receiver lifecycle context.
type lifetimeInternal struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func (l *lifetimeInternal) startInternal(parent context.Context) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
	l.ctx, l.cancel = context.WithCancel(parent)
	return l.ctx
}

func (l *lifetimeInternal) contextInternal() context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ctx
}

func (l *lifetimeInternal) stopInternal() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}

// Ingress coalesces concurrent producers into one pending mailbox signal while
// retaining the newest value. Bind may happen after Send, which lets services
// accept startup triggers before their actor exists.
type Ingress[K comparable, V any] struct {
	mu           sync.Mutex
	kind         K
	value        V
	generation   uint64
	acknowledged uint64
	queued       bool
	handle       *handleInternal
}

// NewIngress creates an unbound coalescing actor ingress.
func NewIngress[K comparable, V any](kind K) *Ingress[K, V] {
	return &Ingress[K, V]{kind: kind}
}

// Bind delivers future signals to handle and flushes one signal queued before startup.
func (i *Ingress[K, V]) bindInternal(handle *handleInternal) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.handle = handle
	if i.queued && handle != nil {
		_ = handle.Send(Message[K, NoPayload]{Kind: i.kind})
	}
}

// Unbind stops delivery to handle without discarding a pending signal.
func (i *Ingress[K, V]) unbindInternal(handle *handleInternal) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.handle == handle {
		i.handle = nil
	}
}

func (i *Ingress[K, V]) replayInternal() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.generation == i.acknowledged || i.queued || i.handle == nil {
		return
	}
	i.queued = true
	_ = i.handle.Send(Message[K, NoPayload]{Kind: i.kind})
}

// Send records value and queues at most one mailbox signal.
func (i *Ingress[K, V]) Send(value V) {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.value = value
	i.generation++
	if i.queued {
		return
	}
	i.queued = true
	if i.handle != nil {
		_ = i.handle.Send(Message[K, NoPayload]{Kind: i.kind})
	}
}

// Begin returns the newest value and its generation while retaining the work
// as pending until Acknowledge commits it.
func (i *Ingress[K, V]) Begin() (V, uint64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.queued = false
	return i.value, i.generation
}

// Acknowledge commits work through generation and replays a newer pending value.
func (i *Ingress[K, V]) Acknowledge(generation uint64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if generation > i.acknowledged {
		i.acknowledged = generation
	}
	if i.generation == i.acknowledged || i.queued || i.handle == nil {
		return
	}
	i.queued = true
	_ = i.handle.Send(Message[K, NoPayload]{Kind: i.kind})
}

// Take returns and immediately acknowledges the newest value.
func (i *Ingress[K, V]) Take() V {
	value, generation := i.Begin()
	i.Acknowledge(generation)
	return value
}

// Pending reports whether an unacknowledged value remains.
func (i *Ingress[K, V]) Pending() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.generation != i.acknowledged
}

// Latest returns the newest value without changing admission state.
func (i *Ingress[K, V]) Latest() V {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.value
}

// Timer sends generation-fenced messages to an actor. It is actor-owned and
// must be stopped when the receiver handles actor.Stopped.
type Timer[K comparable] struct {
	timer      *time.Timer
	generation uint64
}

var timerGenerationInternal atomic.Uint64

// Reset replaces the active timer and returns its generation.
func (t *Timer[K]) Reset(ctx *Context, kind K, delay time.Duration) uint64 {
	if t.timer != nil {
		t.timer.Stop()
	}
	generation := timerGenerationInternal.Add(1)
	t.generation = generation
	engine := ctx.engine
	target := ctx.target
	actorCtx := ctx.Context()
	t.timer = time.AfterFunc(delay, func() {
		if actorCtx.Err() == nil {
			engine.Send(target, Message[K, uint64]{Kind: kind, Value: generation})
		}
	})
	return generation
}

// Current reports whether generation belongs to the latest Reset call.
func (t *Timer[K]) Current(generation uint64) bool {
	return generation == t.generation
}

// Stop cancels the timer if one is active.
func (t *Timer[K]) Stop() {
	t.generation = timerGenerationInternal.Add(1)
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}

// requestInternal sends a typed request through Arcane's promise envelope.
type requestInternal struct {
	message any
	reply   *Promise[requestResultInternal]
}

type requestResultInternal struct {
	message any
	err     error
}

func (h *handleInternal) request[K comparable, Q, R any](ctx context.Context, request Message[K, Q]) (Message[K, R], error) {
	var zero Message[K, R]
	if ctx == nil || h == nil {
		return zero, errors.New("actor request target unavailable")
	}
	reply := NewPromise[requestResultInternal]()
	if err := h.Send(requestInternal{message: request, reply: reply}); err != nil {
		return zero, err
	}
	result, err := h.await(ctx, reply, errors.New("actor stopped"))
	if err != nil {
		return zero, err
	}
	if result.err != nil {
		return zero, result.err
	}
	typed, ok := result.message.(Message[K, R])
	if !ok {
		return zero, fmt.Errorf("unexpected actor response type %T", result.message)
	}
	return typed, nil
}

// Worker describes blocking work owned by an actor but executed outside Receive.
type Worker[K comparable, V any] struct {
	Actor          *Context
	WorkContext    context.Context
	Label          string
	CompletionKind K
	PanicValue     V
	RetryDelay     time.Duration
	ActorStopped   func(V, error)
}

// Start contains panics, joins the work to WaitGroup, and posts one typed
// completion back to the actor. Its completion handler must call Acknowledge.
func (w Worker[K, V]) Start(work func(context.Context) (V, error)) {
	actorCtx := w.Actor.Context()
	engine := w.Actor.engine
	target := w.Actor.target
	workerCtx, cancel := context.WithCancel(w.WorkContext)
	stopActorCancel := context.AfterFunc(actorCtx, cancel)
	w.Actor.workers.Go(func() {
		var (
			value = w.PanicValue
			err   error
		)
		defer cancel()
		defer stopActorCancel()
		defer func() {
			if actorCtx.Err() == nil {
				ack := make(chan struct{}, 1)
				engine.Send(target, Message[K, V]{Kind: w.CompletionKind, Value: value, Err: err, ack: ack})
				select {
				case <-ack:
					return
				case <-actorCtx.Done():
				}
			}
			if w.ActorStopped != nil {
				w.ActorStopped(value, err)
			}
		}()
		for {
			value = w.PanicValue
			err = nil
			func() {
				defer utils.RecoverToError(&err, w.Label)
				value, err = work(workerCtx)
			}()
			if err == nil || w.RetryDelay <= 0 || workerCtx.Err() != nil {
				return
			}
			timer := time.NewTimer(w.RetryDelay)
			select {
			case <-workerCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
}
