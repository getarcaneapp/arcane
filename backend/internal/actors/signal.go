package actors

import (
	"sync"
	"sync/atomic"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// Signal delivers each published value once to every current subscriber.
// Subscribers run sequentially on the publishing goroutine.
type Signal[T any] struct {
	mu          sync.RWMutex
	nextID      atomic.Uint64
	subscribers map[uint64]func(T)
}

// NewSignal creates an empty typed signal.
func NewSignal[T any]() *Signal[T] {
	return &Signal[T]{subscribers: make(map[uint64]func(T))}
}

// Subscribe registers a callback and returns an idempotent unsubscribe function.
func (s *Signal[T]) Subscribe(callback func(T)) func() {
	if s == nil || callback == nil {
		return func() {}
	}
	id := s.nextID.Add(1)
	s.mu.Lock()
	s.subscribers[id] = callback
	s.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subscribers, id)
			s.mu.Unlock()
		})
	}
}

// Publish synchronously delivers value without holding the subscriber lock.
func (s *Signal[T]) Publish(value T) {
	if s == nil {
		return
	}
	s.mu.RLock()
	subscribers := make([]func(T), 0, len(s.subscribers))
	for _, callback := range s.subscribers {
		subscribers = append(subscribers, callback)
	}
	s.mu.RUnlock()

	for _, callback := range subscribers {
		func() {
			var err error
			defer utils.RecoverToError(&err, "actor signal subscriber")
			callback(value)
		}()
	}
}
