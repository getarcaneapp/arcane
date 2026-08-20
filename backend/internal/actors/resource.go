package actors

import (
	"context"
	"log/slog"
	"sync/atomic"

	"emperror.dev/errors"
)

// ErrResourceStopped reports work submitted after terminal resource shutdown.
var ErrResourceStopped = errors.New("actor resource stopped")

// Resource serializes the lifecycle and use of one replaceable value through
// an actor executor. Its value is never accessed outside that mailbox, and its
// zero value represents the absence of a resource.
type Resource[T comparable] struct {
	executor *Executor
	value    T
	terminal atomic.Bool
	stop     func(T) error
}

// NewResource creates an actor-owned replaceable resource.
func NewResource[T comparable](ctx context.Context, runtime *Runtime, kind, id string, maxRestarts int32, stop func(T) error) (*Resource[T], error) {
	if stop == nil {
		return nil, errors.New("resource stop function unavailable")
	}
	executor, err := NewExecutor(ctx, runtime, kind, id, maxRestarts)
	if err != nil {
		return nil, err
	}
	return &Resource[T]{executor: executor, stop: stop}, nil
}

// Restart stops the current value before building and publishing its replacement.
// A non-zero failed build is cleaned up.
func (r *Resource[T]) Restart(ctx context.Context, label string, build func(context.Context) (T, error)) error {
	if build == nil {
		return errors.New("resource build function unavailable")
	}
	if r.terminal.Load() {
		return ErrResourceStopped
	}
	_, err := r.executor.Execute(ctx, label, func(workCtx context.Context) (NoPayload, error) {
		if r.terminal.Load() {
			return NoPayload{}, ErrResourceStopped
		}
		stopErr := r.clearInternal()
		next, buildErr := build(workCtx)
		if buildErr != nil {
			var zero T
			if next != zero {
				buildErr = errors.Combine(buildErr, r.stop(next))
			}
			return NoPayload{}, errors.Combine(stopErr, buildErr)
		}
		r.value = next
		if stopErr != nil {
			slog.WarnContext(workCtx, "failed to stop replaced actor resource", "label", label, "error", stopErr)
		}
		return NoPayload{}, nil
	}, nil)
	return err
}

// Do serializes work with Restart and Stop. Work is skipped after terminal stop.
func (r *Resource[T]) Do(ctx context.Context, label string, work func(context.Context, T) error) error {
	if work == nil {
		return errors.New("resource work function unavailable")
	}
	if r.terminal.Load() {
		return ErrResourceStopped
	}
	_, err := r.executor.Execute(ctx, label, func(workCtx context.Context) (NoPayload, error) {
		if r.terminal.Load() {
			return NoPayload{}, ErrResourceStopped
		}
		return NoPayload{}, work(workCtx, r.value)
	}, nil)
	return err
}

// Clear stops and forgets the current value while keeping the resource reusable.
func (r *Resource[T]) Clear(ctx context.Context, label string) error {
	if r.terminal.Load() {
		return ErrResourceStopped
	}
	_, err := r.executor.Execute(ctx, label, func(context.Context) (NoPayload, error) {
		if r.terminal.Load() {
			return NoPayload{}, ErrResourceStopped
		}
		return NoPayload{}, r.clearInternal()
	}, nil)
	return err
}

// Stop permanently fences replacement, stops the current value, and joins the actor.
func (r *Resource[T]) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("resource stop context unavailable")
	}
	r.terminal.Store(true)
	task, submitErr := r.executor.Submit(context.WithoutCancel(ctx), "stop actor resource", func(context.Context) (NoPayload, error) {
		return NoPayload{}, r.clearInternal()
	}, nil)
	var clearErr error
	if submitErr == nil {
		_, clearErr = task.Wait(ctx)
	}
	actorErr := r.executor.Stop(ctx)
	if submitErr != nil {
		// Submit only fails after the executor can no longer own the value, so
		// terminal cleanup must stop it directly instead of leaking it.
		clearErr = r.clearInternal()
	}
	return errors.Combine(submitErr, clearErr, actorErr)
}

func (r *Resource[T]) clearInternal() error {
	var zero T
	if r.value == zero {
		return nil
	}
	current := r.value
	r.value = zero
	return r.stop(current)
}
