package actors

import "sync/atomic"

// Snapshot publishes immutable copies of a value for lock-free readers.
type Snapshot[T any] struct {
	value atomic.Pointer[T]
}

// Store replaces the published value with an immutable copy.
func (s *Snapshot[T]) Store(value T) {
	if s == nil {
		return
	}
	s.value.Store(&value)
}

// Load returns a copy of the latest published value.
func (s *Snapshot[T]) Load() (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	value := s.value.Load()
	if value == nil {
		return zero, false
	}
	return *value, true
}
