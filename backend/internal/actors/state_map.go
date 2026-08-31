package actors

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"

	"emperror.dev/errors"
)

// StateMap owns mutable keyed state while publishing immutable snapshots for readers.
type StateMap[K comparable, V any] struct {
	executor *Executor
	values   map[K]V
	snapshot atomic.Pointer[map[K]V]
	mu       sync.Mutex
}

type stateMapValueInternal[V any] struct {
	value V
	found bool
}

// NewStateMap creates a locally serialized state map for callers without an actor runtime.
func NewStateMap[K comparable, V any]() *StateMap[K, V] {
	state := &StateMap[K, V]{values: make(map[K]V)}
	state.publishInternal()
	return state
}

// NewActorStateMap creates a state map whose mutations use the shared actor runtime.
func NewActorStateMap[K comparable, V any](ctx context.Context, runtime *Runtime, kind, id string, maxRestarts int32) (*StateMap[K, V], error) {
	executor, err := NewExecutor(ctx, runtime, kind, id, maxRestarts)
	if err != nil {
		return nil, err
	}
	state := &StateMap[K, V]{executor: executor, values: make(map[K]V)}
	state.publishInternal()
	return state, nil
}

// Get reads one value from the latest immutable snapshot without entering the mailbox.
func (s *StateMap[K, V]) Get(key K) (V, bool) {
	var zero V
	if s == nil {
		return zero, false
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		return zero, false
	}
	value, ok := (*snapshot)[key]
	return value, ok
}

// Values reads every value from the latest immutable snapshot.
func (s *StateMap[K, V]) Values() []V {
	if s == nil {
		return nil
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		return nil
	}
	values := make([]V, 0, len(*snapshot))
	for _, value := range *snapshot {
		values = append(values, value)
	}
	return values
}

// Apply serializes a mutation and publishes it atomically when changed is true.
func (s *StateMap[K, V]) Apply(ctx context.Context, label string, mutate func(map[K]V) (changed bool, err error)) error {
	if mutate == nil {
		return errors.New("state map mutation unavailable")
	}
	_, err := s.ApplyTyped(ctx, label, func(values map[K]V) (NoPayload, bool, error) {
		changed, err := mutate(values)
		return NoPayload{}, changed, err
	})
	return err
}

// ApplyTyped serializes a typed mutation and publishes its resulting state.
func (s *StateMap[K, V]) ApplyTyped[R any](
	ctx context.Context,
	label string,
	mutate func(map[K]V) (result R, changed bool, err error),
) (R, error) {
	var zero R
	if s == nil {
		return zero, errors.New("state map unavailable")
	}
	if mutate == nil {
		return zero, errors.New("state map mutation unavailable")
	}
	work := func(context.Context) (R, error) {
		completed := false
		defer func() {
			if !completed {
				s.publishInternal()
			}
		}()
		result, changed, err := mutate(s.values)
		completed = true
		if changed || err != nil {
			s.publishInternal()
		}
		return result, err
	}
	if s.executor != nil {
		return s.executor.Execute(ctx, label, work, nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return work(ctx)
}

// Store publishes a value and returns the value it replaced, if any.
func (s *StateMap[K, V]) Store(ctx context.Context, label string, key K, value V) (V, bool, error) {
	result, err := s.ApplyTyped(ctx, label, func(values map[K]V) (stateMapValueInternal[V], bool, error) {
		previous, replaced := values[key]
		values[key] = value
		return stateMapValueInternal[V]{value: previous, found: replaced}, true, nil
	})
	return result.value, result.found, err
}

// Remove deletes and returns one value.
func (s *StateMap[K, V]) Remove(ctx context.Context, label string, key K) (V, bool, error) {
	result, err := s.ApplyTyped(ctx, label, func(values map[K]V) (stateMapValueInternal[V], bool, error) {
		value, removed := values[key]
		if removed {
			delete(values, key)
		}
		return stateMapValueInternal[V]{value: value, found: removed}, removed, nil
	})
	return result.value, result.found, err
}

// RemoveWhere atomically removes every value selected by predicate.
func (s *StateMap[K, V]) RemoveWhere(ctx context.Context, label string, predicate func(K, V) bool) ([]V, error) {
	if predicate == nil {
		return nil, errors.New("state map predicate unavailable")
	}
	return s.ApplyTyped(ctx, label, func(values map[K]V) ([]V, bool, error) {
		removed := make([]V, 0)
		for key, value := range values {
			if predicate(key, value) {
				delete(values, key)
				removed = append(removed, value)
			}
		}
		return removed, len(removed) > 0, nil
	})
}

// Drain atomically removes and returns every value.
func (s *StateMap[K, V]) Drain(ctx context.Context, label string) ([]V, error) {
	return s.RemoveWhere(ctx, label, func(K, V) bool { return true })
}

// Stop drains actor-backed mutations and joins the shared executor.
func (s *StateMap[K, V]) Stop(ctx context.Context) error {
	if s == nil || s.executor == nil {
		return nil
	}
	return s.executor.Stop(ctx)
}

func (s *StateMap[K, V]) publishInternal() {
	snapshot := make(map[K]V, len(s.values))
	maps.Copy(snapshot, s.values)
	s.snapshot.Store(&snapshot)
}
