package concurrency

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

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
			defer utils.RecoverToError(&err, "signal subscriber")
			callback(value)
		}()
	}
}

// StartSupervised starts a background operation and returns its cancel-and-join function.
func StartSupervised(ctx context.Context, label string, run func(context.Context) error) (func(context.Context) error, error) {
	if ctx == nil || run == nil {
		return nil, errors.New("background operation dependencies unavailable")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		delay := 5 * time.Second
		unexpected := 0
		for runCtx.Err() == nil {
			started := time.Now()
			var runErr error
			panicked := true
			func() { defer utils.RecoverToError(&runErr, label); runErr = run(runCtx); panicked = false }()
			if runCtx.Err() != nil {
				return
			}
			if panicked || runErr == nil {
				unexpected++
			}
			if runErr == nil {
				runErr = errors.New("background operation exited unexpectedly")
			}
			if time.Since(started) > 5*time.Minute {
				delay = 5 * time.Second
				unexpected = 0
			}
			if unexpected >= 3 {
				slog.ErrorContext(runCtx, "Background operation requires attention", "label", label, "error", runErr)
				return
			}
			jitter, err := rand.Int(rand.Reader, big.NewInt(int64(delay/2)))
			if err != nil {
				slog.ErrorContext(runCtx, "Failed to schedule background retry", "label", label, "error", err)
				return
			}
			wait := delay/2 + time.Duration(jitter.Int64())
			slog.WarnContext(runCtx, "Background operation failed; retrying", "label", label, "error", runErr, "retryIn", wait)
			timer := time.NewTimer(wait)
			select {
			case <-runCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, 5*time.Minute)
		}
	}()
	return func(stopCtx context.Context) error {
		cancel()
		select {
		case <-done:
			return nil
		case <-stopCtx.Done():
			return stopCtx.Err()
		}
	}, nil
}
