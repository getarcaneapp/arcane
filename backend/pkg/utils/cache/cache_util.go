package cache

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type ErrStale struct {
	Err error
}

func (e *ErrStale) Error() string { return "stale cache value: " + e.Err.Error() }
func (e *ErrStale) Unwrap() error { return e.Err }

type Cache[T any] struct {
	ttl time.Duration

	mu  sync.RWMutex
	val T
	exp time.Time
	set bool

	sf singleflight.Group
}

func New[T any](ttl time.Duration) *Cache[T] {
	return &Cache[T]{ttl: ttl}
}

func (c *Cache[T]) GetOrFetch(ctx context.Context, fetch func(ctx context.Context) (T, error)) (T, error) {
	c.mu.RLock()
	if c.set && time.Now().Before(c.exp) {
		v := c.val
		c.mu.RUnlock()
		return v, nil
	}

	hasStale := c.set
	stale := c.val
	c.mu.RUnlock()

	res, err, _ := c.sf.Do("singleton", func() (any, error) {
		v, err := fetch(ctx)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.val = v
		c.set = true
		c.exp = time.Now().Add(c.ttl)
		c.mu.Unlock()
		return v, nil
	})
	if err != nil {
		if hasStale {
			return stale, &ErrStale{Err: err}
		}
		var zero T
		return zero, err
	}

	v, _ := res.(T)
	return v, nil
}
