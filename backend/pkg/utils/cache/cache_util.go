package cache

import (
	"context"
	"fmt"
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

type KeyedCache[K comparable, T any] struct {
	mu      sync.RWMutex
	entries map[K]T
	sf      singleflight.Group
}

func New[T any](ttl time.Duration) *Cache[T] {
	return &Cache[T]{ttl: ttl}
}

func NewKeyed[K comparable, T any]() *KeyedCache[K, T] {
	return &KeyedCache[K, T]{
		entries: make(map[K]T),
	}
}

func (c *KeyedCache[K, T]) GetOrFetch(
	ctx context.Context,
	key K,
	valid func(cached T) bool,
	fetch func(ctx context.Context) (T, error),
) (T, error) {
	c.mu.RLock()
	cached, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && (valid == nil || valid(cached)) {
		return cached, nil
	}

	res, err, _ := c.sf.Do(fmt.Sprint(key), func() (any, error) {
		c.mu.RLock()
		cached, ok := c.entries[key]
		c.mu.RUnlock()
		if ok && (valid == nil || valid(cached)) {
			return cached, nil
		}

		v, err := fetch(ctx)
		if err != nil {
			var zero T
			return zero, err
		}

		c.mu.Lock()
		c.entries[key] = v
		c.mu.Unlock()
		return v, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}

	v, _ := res.(T)
	return v, nil
}

func (c *KeyedCache[K, T]) Invalidate(key K) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *KeyedCache[K, T]) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[K]T)
	c.mu.Unlock()
}

func (c *Cache[T]) GetOrFetch(ctx context.Context, fetch func(ctx context.Context) (T, error)) (T, error) {
	c.mu.RLock()
	if c.set && (c.ttl <= 0 || time.Now().Before(c.exp)) {
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
		if c.ttl > 0 {
			c.exp = time.Now().Add(c.ttl)
		}
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

func (c *Cache[T]) Invalidate() {
	c.mu.Lock()
	c.set = false
	var zero T
	c.val = zero
	c.exp = time.Time{}
	c.mu.Unlock()
}
