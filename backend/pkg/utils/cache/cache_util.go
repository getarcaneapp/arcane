package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/samber/mo"
	"golang.org/x/sync/singleflight"
)

type StaleError struct {
	Err error
}

func (e *StaleError) Error() string { return "stale cache value: " + e.Err.Error() }
func (e *StaleError) Unwrap() error { return e.Err }

type Cache[T any] struct {
	ttl time.Duration

	mu         sync.RWMutex
	val        T
	exp        time.Time
	set        bool
	generation uint64

	sf singleflight.Group
}

type cacheSnapshotInternal[T any] struct {
	value      T
	set        bool
	fresh      bool
	generation uint64
}

type KeyedCache[K comparable, T any] struct {
	mu             sync.RWMutex
	entries        map[K]T
	keyGenerations map[K]uint64
	allGeneration  uint64
	sf             singleflight.Group
}

func New[T any](ttl time.Duration) *Cache[T] {
	return &Cache[T]{ttl: ttl}
}

func NewKeyed[K comparable, T any]() *KeyedCache[K, T] {
	return &KeyedCache[K, T]{
		entries:        make(map[K]T),
		keyGenerations: make(map[K]uint64),
	}
}

func (c *Cache[T]) snapshotInternal() cacheSnapshotInternal[T] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cacheSnapshotInternal[T]{
		value:      c.val,
		set:        c.set,
		fresh:      c.set && (c.ttl <= 0 || time.Now().Before(c.exp)),
		generation: c.generation,
	}
}

func retryInvalidatedFetchInternal(ctx context.Context, err error) (bool, error) {
	if !common.IsCacheInvalidatedDuringFetchError(err) {
		return false, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	return true, nil
}

// Get returns the cached value for key, if present.
func (c *KeyedCache[K, T]) Get(key K) mo.Option[T] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cached, ok := c.entries[key]
	return mo.TupleToOption(cached, ok)
}

// Set stores value for key, replacing any existing entry.
func (c *KeyedCache[K, T]) Set(key K, value T) {
	c.mu.Lock()
	c.entries[key] = value
	c.mu.Unlock()
}

func (c *KeyedCache[K, T]) GetOrFetch(
	ctx context.Context,
	key K,
	valid func(cached T) bool,
	fetch func(ctx context.Context) (T, error),
) (T, error) {
	for {
		c.mu.RLock()
		cached, ok := c.entries[key]
		allGeneration := c.allGeneration
		keyGeneration := c.keyGenerations[key]
		c.mu.RUnlock()
		if ok && (valid == nil || valid(cached)) {
			return cached, nil
		}

		flightKey := fmt.Sprintf("%#v:%d:%d", key, allGeneration, keyGeneration)
		res, err, _ := c.sf.Do(flightKey, func() (any, error) {
			c.mu.RLock()
			cached, ok := c.entries[key]
			generationCurrent := c.allGeneration == allGeneration && c.keyGenerations[key] == keyGeneration
			c.mu.RUnlock()
			if !generationCurrent {
				return nil, &common.CacheInvalidatedDuringFetchError{}
			}
			if ok && (valid == nil || valid(cached)) {
				return cached, nil
			}

			v, err := fetch(ctx)
			if err != nil {
				return nil, err
			}

			c.mu.Lock()
			generationCurrent = c.allGeneration == allGeneration && c.keyGenerations[key] == keyGeneration
			if generationCurrent {
				c.entries[key] = v
			}
			c.mu.Unlock()
			if !generationCurrent {
				return nil, &common.CacheInvalidatedDuringFetchError{}
			}
			return v, nil
		})
		retry, err := retryInvalidatedFetchInternal(ctx, err)
		if retry {
			continue
		}
		if err != nil {
			var zero T
			return zero, err
		}

		v, _ := res.(T)
		return v, nil
	}
}

func (c *KeyedCache[K, T]) Invalidate(key K) {
	c.mu.Lock()
	delete(c.entries, key)
	c.keyGenerations[key]++
	c.mu.Unlock()
}

func (c *KeyedCache[K, T]) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[K]T)
	c.keyGenerations = make(map[K]uint64)
	c.allGeneration++
	c.mu.Unlock()
}

func (c *Cache[T]) GetOrFetch(ctx context.Context, fetch func(ctx context.Context) (T, error)) (T, error) {
	for {
		snapshot := c.snapshotInternal()
		if snapshot.fresh {
			return snapshot.value, nil
		}

		res, err, _ := c.sf.Do(fmt.Sprintf("singleton:%d", snapshot.generation), func() (any, error) {
			current := c.snapshotInternal()
			if current.fresh {
				return current.value, nil
			}
			if current.generation != snapshot.generation {
				return nil, &common.CacheInvalidatedDuringFetchError{}
			}

			v, err := fetch(ctx)
			if err != nil {
				return nil, err
			}

			c.mu.Lock()
			generationCurrent := c.generation == snapshot.generation
			if generationCurrent {
				c.val = v
				c.set = true
				if c.ttl > 0 {
					c.exp = time.Now().Add(c.ttl)
				}
			}
			c.mu.Unlock()
			if !generationCurrent {
				return nil, &common.CacheInvalidatedDuringFetchError{}
			}
			return v, nil
		})
		retry, err := retryInvalidatedFetchInternal(ctx, err)
		if retry {
			continue
		}
		if err != nil {
			current := c.snapshotInternal()
			if snapshot.set && current.generation == snapshot.generation {
				return snapshot.value, &StaleError{Err: err}
			}
			var zero T
			return zero, err
		}

		v, _ := res.(T)
		return v, nil
	}
}

func (c *Cache[T]) Invalidate() {
	c.mu.Lock()
	c.generation++
	c.set = false
	var zero T
	c.val = zero
	c.exp = time.Time{}
	c.mu.Unlock()
}
