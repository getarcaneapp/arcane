package ratelimit

import (
	"context"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
	kit "kit.getarcane.app"
)

type (
	RegistryRateLimiter struct {
		semaphores   map[string]*semaphore.Weighted
		limits       map[string]int64
		defaultLimit int64
		mu           sync.RWMutex
	}
)

const (
	defaultRegistryConcurrencyLimit int64 = 3
)

var defaultRegistryLimits = map[string]int64{
	"ghcr.io":   3,
	"docker.io": 5,
	"quay.io":   4,
	"gcr.io":    4,
}

func NewRegistryRateLimiter() *RegistryRateLimiter {
	return &RegistryRateLimiter{
		semaphores:   make(map[string]*semaphore.Weighted),
		limits:       defaultRegistryLimits,
		defaultLimit: defaultRegistryConcurrencyLimit,
	}
}

func (r *RegistryRateLimiter) Acquire(ctx context.Context, registry string) error {
	sem := r.getSemaphore(registry)
	return sem.Acquire(ctx, 1)
}

func (r *RegistryRateLimiter) Release(registry string) {
	normalized := normalizeRegistryKey(registry)
	r.mu.RLock()
	sem, ok := r.semaphores[normalized]
	r.mu.RUnlock()
	if ok {
		sem.Release(1)
	}
}

func (r *RegistryRateLimiter) getSemaphore(registry string) *semaphore.Weighted {
	normalized := normalizeRegistryKey(registry)

	r.mu.RLock()
	sem, ok := r.semaphores[normalized]
	r.mu.RUnlock()
	if ok {
		return sem
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if sem, ok = r.semaphores[normalized]; ok {
		return sem
	}

	limit := r.limitForRegistry(normalized)
	sem = semaphore.NewWeighted(limit)
	r.semaphores[normalized] = sem
	return sem
}

func (r *RegistryRateLimiter) limitForRegistry(registry string) int64 {
	if limit, ok := r.limits[registry]; ok {
		return limit
	}
	if strings.Contains(registry, ".ecr.") && strings.Contains(registry, ".amazonaws.com") {
		return 4
	}
	return r.defaultLimit
}

func normalizeRegistryKey(registry string) string {
	normalized := strings.ToLower(strings.TrimSpace(registry))
	return kit.Ternary(normalized == "registry-1.docker.io" || normalized == "index.docker.io", "docker.io", normalized)
}
