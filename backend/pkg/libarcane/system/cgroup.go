// Package system encapsulates host-level resource probing used by the WebSocket system stats
// endpoint: cgroup limits and GPU detection/stats. The types here own their own caching and
// synchronization so handlers can hold them as plain fields.
package system

import (
	"sync"
	"time"

	docker "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
)

// CgroupCache memoizes the result of cgroup limit detection for a configurable TTL.
// A single in-flight refresh is shared across concurrent callers (singleflight semantics
// via the write lock), so a stampede of system-stats samplers never duplicates the work.
type CgroupCache struct {
	mu        sync.RWMutex
	value     *docker.CgroupLimits
	detected  bool
	timestamp time.Time
	ttl       time.Duration
	detector  func() (*docker.CgroupLimits, error)
}

// NewCgroupCache returns a cache that caches detection results for ttl using the
// default docker.DetectCgroupLimits detector.
func NewCgroupCache(ttl time.Duration) *CgroupCache {
	return NewCgroupCacheWithDetector(ttl, docker.DetectCgroupLimits)
}

// NewCgroupCacheWithDetector returns a cache backed by a custom detector. Useful when
// callers (e.g. tests) need to control detection behaviour or simulate failures.
func NewCgroupCacheWithDetector(ttl time.Duration, detector func() (*docker.CgroupLimits, error)) *CgroupCache {
	return &CgroupCache{ttl: ttl, detector: detector}
}

// Get returns the cached cgroup limits, refreshing if the entry is older than the TTL.
// Returns nil when no cgroup limits have been detected (host is not running under cgroups).
func (c *CgroupCache) Get() *docker.CgroupLimits {
	c.mu.RLock()
	value, detected, fresh := c.value, c.detected, time.Since(c.timestamp) < c.ttl
	c.mu.RUnlock()
	if fresh {
		if detected {
			return value
		}
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.timestamp) < c.ttl {
		if c.detected {
			return c.value
		}
		return nil
	}

	limits, err := c.detector()
	c.timestamp = time.Now()
	if err != nil {
		c.value = nil
		c.detected = false
		return nil
	}
	c.value = limits
	c.detected = true
	return c.value
}
