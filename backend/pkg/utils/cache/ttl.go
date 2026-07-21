package cache

import (
	"sync"
	"time"

	"github.com/samber/mo"
)

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

// TTL is a concurrency-safe in-memory cache with per-entry expiration.
type TTL[V any] struct {
	mu        sync.RWMutex
	entries   map[string]*entry[V]
	ttl       time.Duration
	lastSweep time.Time
}

func NewTTL[V any](ttl time.Duration) *TTL[V] {
	return &TTL[V]{
		entries: make(map[string]*entry[V]),
		ttl:     ttl,
	}
}

func (c *TTL[V]) Get(key string) mo.Option[V] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, exists := c.entries[key]
	e, ok := mo.TupleToOption(e, exists).Get()
	if !ok || time.Now().After(e.expiresAt) {
		return mo.None[V]()
	}
	return mo.Some(e.value)
}

func (c *TTL[V]) Put(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if now.Sub(c.lastSweep) > c.ttl {
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		c.lastSweep = now
	}
	c.entries[key] = &entry[V]{
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
}

// DeleteFunc removes all entries where the predicate returns true.
func (c *TTL[V]) DeleteFunc(fn func(key string, value V) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if fn(k, e.value) {
			delete(c.entries, k)
		}
	}
}

// Delete removes the entry for key, if present.
func (c *TTL[V]) Delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}
