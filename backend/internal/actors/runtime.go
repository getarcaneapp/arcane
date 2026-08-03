// Package actors owns Arcane's in-process actor runtime and its shared safety rails.
package actors

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"

	"emperror.dev/errors"
	"github.com/anthdm/hollywood/actor"
	"go.uber.org/fx"
)

const actorMonitorID = "arcane-monitor"

type monitorReadyInternal struct{}

// Runtime owns the shared Hollywood engine and observes actor failures.
type Runtime struct {
	engine              *actor.Engine
	monitorPID          *actor.PID
	deadLetters         atomic.Uint64
	restarts            atomic.Uint64
	maxRestartsExceeded atomic.Uint64
	duplicateIDs        atomic.Uint64
	policyMu            sync.Mutex
	policies            map[string]actorPolicyInternal
}

type actorPolicyInternal struct {
	maxRestarts int32
	handle      *handleInternal
}

// handleInternal identifies a spawned actor and reports when its process terminates.
type handleInternal struct {
	engine   *actor.Engine
	pid      *actor.PID
	done     chan struct{}
	doneOnce sync.Once
	stopOnce sync.Once
}

// Done closes when the actor process permanently stops.
func (h *handleInternal) Done() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.done
}

// Send queues a message for the actor while hiding Hollywood's engine and PID
// from services that only need to communicate with their owned process.
func (h *handleInternal) Send(message any) error {
	if h == nil || h.engine == nil || h.pid == nil {
		return errors.New("actor handle unavailable")
	}
	select {
	case <-h.done:
		return errors.New("actor stopped")
	default:
		h.engine.Send(h.pid, message)
		return nil
	}
}

// Stop drains the actor mailbox, initiates termination exactly once, and waits
// for the process to exit. A canceled wait does not cancel the queued stop.
func (h *handleInternal) Stop(ctx context.Context) error {
	if h == nil || h.engine == nil || h.pid == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("actor stop context unavailable")
	}
	h.stopOnce.Do(func() { h.engine.Poison(h.pid) })
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *handleInternal) stopInternal() {
	if h != nil {
		h.doneOnce.Do(func() { close(h.done) })
	}
}

// NewRuntime constructs the shared actor engine and registers its lifecycle monitor.
func NewRuntime(appCtx context.Context, lc fx.Lifecycle) (*Runtime, error) {
	engine, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{
		engine:   engine,
		policies: make(map[string]actorPolicyInternal),
	}
	ready := make(chan struct{})
	var readyOnce sync.Once
	runtime.monitorPID = engine.Spawn(
		func() actor.Receiver {
			return &runtimeMonitorInternal{
				runtime:   runtime,
				ready:     ready,
				readyOnce: &readyOnce,
			}
		},
		"runtime-monitor",
		actor.WithID(actorMonitorID),
		actor.WithContext(appCtx),
		actor.WithMaxRestarts(math.MaxInt32),
	)
	engine.Subscribe(runtime.monitorPID)
	engine.BroadcastEvent(monitorReadyInternal{})

	select {
	case <-ready:
	case <-appCtx.Done():
		<-engine.Poison(runtime.monitorPID).Done()
		return nil, appCtx.Err()
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			engine.Unsubscribe(runtime.monitorPID)
			stopped := engine.PoisonCtx(ctx, runtime.monitorPID)
			select {
			case <-stopped.Done():
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	return runtime, nil
}

// startInternal starts a Hollywood receiver with a deterministic ID and an
// Arcane-enforced restart limit.
// Hollywood v1.0.5 panics while cleaning up its native max-restart path, so the
// shared monitor enforces the same ceiling and stops the actor safely instead.
func (r *Runtime) startInternal(producer actor.Producer, kind, id string, maxRestarts int32, opts ...actor.OptFunc) (*handleInternal, error) {
	if r == nil || r.engine == nil {
		return nil, errors.New("actor runtime unavailable")
	}
	if producer == nil {
		return nil, errors.New("actor producer unavailable")
	}

	policyKey := actor.NewPID(r.engine.Address(), kind+"/"+id).String()
	r.policyMu.Lock()
	if _, exists := r.policies[policyKey]; exists || r.engine.Registry.GetPID(kind, id) != nil {
		r.policyMu.Unlock()
		return nil, fmt.Errorf("actor ID already in use: %s/%s", kind, id)
	}
	handle := &handleInternal{
		engine: r.engine,
		pid:    actor.NewPID(r.engine.Address(), kind+"/"+id),
		done:   make(chan struct{}),
	}
	r.policies[policyKey] = actorPolicyInternal{maxRestarts: maxRestarts, handle: handle}
	r.policyMu.Unlock()

	options := append([]actor.OptFunc{}, opts...)
	options = append(options, actor.WithID(id), actor.WithMaxRestarts(math.MaxInt32))
	handle.pid = r.engine.Spawn(producer, kind, options...)
	if handle.pid.String() != policyKey {
		r.policyMu.Lock()
		delete(r.policies, policyKey)
		r.policyMu.Unlock()
		r.engine.Stop(handle.pid)
		return nil, fmt.Errorf("actor PID mismatch: got %s, expected %s", handle.pid.String(), policyKey)
	}
	return handle, nil
}

// DeadLetterCount returns the number of undeliverable actor messages observed.
func (r *Runtime) DeadLetterCount() uint64 {
	if r == nil {
		return 0
	}
	return r.deadLetters.Load()
}

// RestartCount returns the number of actor restarts observed.
func (r *Runtime) RestartCount() uint64 {
	if r == nil {
		return 0
	}
	return r.restarts.Load()
}

// MaxRestartsExceededCount returns the number of permanently stopped actors observed.
func (r *Runtime) MaxRestartsExceededCount() uint64 {
	if r == nil {
		return 0
	}
	return r.maxRestartsExceeded.Load()
}

// DuplicateIDCount returns the number of duplicate actor registrations observed.
func (r *Runtime) DuplicateIDCount() uint64 {
	if r == nil {
		return 0
	}
	return r.duplicateIDs.Load()
}

type runtimeMonitorInternal struct {
	runtime   *Runtime
	ready     chan struct{}
	readyOnce *sync.Once
}

func (m *runtimeMonitorInternal) Receive(ctx *actor.Context) {
	switch event := ctx.Message().(type) {
	case actor.Started:
		ctx.Engine().Subscribe(ctx.PID())

	case monitorReadyInternal:
		m.readyOnce.Do(func() { close(m.ready) })

	case actor.ActorRestartedEvent:
		if event.PID == nil {
			slog.ErrorContext(ctx.Context(), "actor restarted event omitted PID")
			return
		}
		m.runtime.policyMu.Lock()
		policy, tracked := m.runtime.policies[event.PID.String()]
		m.runtime.policyMu.Unlock()
		if tracked && event.Restarts > policy.maxRestarts {
			m.runtime.maxRestartsExceeded.Add(1)
			slog.ErrorContext(ctx.Context(), "actor permanently stopped after exceeding restart limit", "pid", event.PID.String(), "reason", event.Reason, "restarts", event.Restarts, "maxRestarts", policy.maxRestarts, "stack", string(event.Stacktrace))
			ctx.Engine().Stop(event.PID)
			return
		}
		m.runtime.restarts.Add(1)
		slog.ErrorContext(ctx.Context(), "actor crashed and restarted", "pid", event.PID.String(), "reason", event.Reason, "restarts", event.Restarts, "stack", string(event.Stacktrace))

	case actor.ActorMaxRestartsExceededEvent:
		if event.PID == nil {
			slog.ErrorContext(ctx.Context(), "actor max-restarts event omitted PID")
			return
		}
		m.runtime.maxRestartsExceeded.Add(1)
		slog.ErrorContext(ctx.Context(), "actor permanently stopped after exceeding restart limit", "pid", event.PID.String())

	case actor.ActorStoppedEvent:
		if event.PID == nil {
			slog.ErrorContext(ctx.Context(), "actor stopped event omitted PID")
			return
		}
		m.runtime.policyMu.Lock()
		policy, tracked := m.runtime.policies[event.PID.String()]
		if tracked {
			delete(m.runtime.policies, event.PID.String())
		}
		m.runtime.policyMu.Unlock()
		if tracked {
			policy.handle.stopInternal()
		}

	case actor.ActorDuplicateIdEvent:
		m.runtime.duplicateIDs.Add(1)
		pid := "<nil>"
		if event.PID != nil {
			pid = event.PID.String()
		}
		slog.ErrorContext(ctx.Context(), "duplicate actor ID rejected", "pid", pid)

	case actor.DeadLetterEvent:
		m.runtime.deadLetters.Add(1)
		target := "<nil>"
		if event.Target != nil {
			target = event.Target.String()
		}
		slog.ErrorContext(ctx.Context(), "actor message sent to dead letter", "target", target, "messageType", fmt.Sprintf("%T", event.Message))

	case actor.Stopped:
		ctx.Engine().Unsubscribe(ctx.PID())
	}
}
