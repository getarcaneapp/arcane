package actors

import (
	"context"

	"emperror.dev/errors"
)

// State serializes mutations through an actor and publishes immutable snapshots.
type State[T any] struct {
	executor *Executor
	value    T
	clone    func(T) T
	snapshot Snapshot[T]
}

// NewState creates actor-owned state with a caller-defined snapshot clone.
func NewState[T any](ctx context.Context, runtime *Runtime, kind, id string, maxRestarts int32, initial T, clone func(T) T) (*State[T], error) {
	if clone == nil {
		return nil, errors.New("actor state clone unavailable")
	}
	executor, err := NewExecutor(ctx, runtime, kind, id, maxRestarts)
	if err != nil {
		return nil, err
	}
	state := &State[T]{
		executor: executor,
		value:    initial,
		clone:    clone,
	}
	state.snapshot.Store(clone(initial))
	return state, nil
}

// Apply serializes one mutation and publishes the resulting state.
func (s *State[T]) Apply(ctx context.Context, label string, mutate func(context.Context, *T) error) error {
	if s == nil || s.executor == nil {
		return errors.New("actor state unavailable")
	}
	if mutate == nil {
		return errors.New("actor state mutation unavailable")
	}
	_, err := s.executor.Execute(ctx, label, func(actorCtx context.Context) (NoPayload, error) {
		defer func() {
			s.snapshot.Store(s.clone(s.value))
		}()
		return NoPayload{}, mutate(actorCtx, &s.value)
	}, nil)
	return err
}

// Load returns the latest immutable state snapshot.
func (s *State[T]) Load() (T, bool) {
	if s == nil {
		var zero T
		return zero, false
	}
	return s.snapshot.Load()
}

// Stop drains pending mutations and joins the state actor.
func (s *State[T]) Stop(ctx context.Context) error {
	if s == nil || s.executor == nil {
		return nil
	}
	return s.executor.Stop(ctx)
}

// Done closes after the state actor terminates.
func (s *State[T]) Done() <-chan struct{} {
	if s == nil || s.executor == nil {
		return nil
	}
	return s.executor.Done()
}
