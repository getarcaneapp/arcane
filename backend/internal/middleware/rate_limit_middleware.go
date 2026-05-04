package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// ipRateLimiter tracks per-client token bucket limiters keyed by client IP.
type ipRateLimiter struct {
	mu         sync.Mutex
	limiters   map[string]*limiterEntry
	rate       rate.Limit
	burst      int
	ttl        time.Duration
	lastSweep  time.Time
	maxEntries int
}

type limiterEntry struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newIPRateLimiterInternal(r rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters:   make(map[string]*limiterEntry),
		rate:       r,
		burst:      burst,
		ttl:        10 * time.Minute,
		maxEntries: 10000,
	}
}

func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.lastSweep) > time.Minute || len(l.limiters) > l.maxEntries {
		for k, e := range l.limiters {
			if now.Sub(e.seen) > l.ttl {
				delete(l.limiters, k)
			}
		}
		l.trimToMaxEntriesInternal(key)
		l.lastSweep = now
	}

	entry, ok := l.limiters[key]
	if !ok {
		l.trimForNewEntryInternal(key)
		entry = &limiterEntry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.limiters[key] = entry
	}
	entry.seen = now
	return entry.limiter.Allow()
}

func (l *ipRateLimiter) trimForNewEntryInternal(key string) {
	if l.maxEntries <= 0 || len(l.limiters) < l.maxEntries {
		return
	}
	l.evictOldestEntriesInternal(len(l.limiters)-l.maxEntries+1, key)
}

func (l *ipRateLimiter) trimToMaxEntriesInternal(protectedKey string) {
	if l.maxEntries <= 0 || len(l.limiters) <= l.maxEntries {
		return
	}
	l.evictOldestEntriesInternal(len(l.limiters)-l.maxEntries, protectedKey)
}

func (l *ipRateLimiter) evictOldestEntriesInternal(count int, protectedKey string) {
	if count <= 0 {
		return
	}

	entries := make([]struct {
		key  string
		seen time.Time
	}, 0, len(l.limiters))
	for key, entry := range l.limiters {
		if key == protectedKey {
			continue
		}
		entries = append(entries, struct {
			key  string
			seen time.Time
		}{key: key, seen: entry.seen})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].seen.Before(entries[j].seen)
	})

	for i := 0; i < count && i < len(entries); i++ {
		delete(l.limiters, entries[i].key)
	}
}

// PerIPRateLimit returns a Gin middleware that limits requests per client IP
// to the given rate and burst. It responds with 429 when the limit is
// exceeded. It is intended for public unauthenticated or weakly-authenticated
// endpoints such as agent mTLS enrollment.
func PerIPRateLimit(perMinute int, burst int) gin.HandlerFunc {
	if perMinute <= 0 {
		perMinute = 10
	}
	if burst <= 0 {
		burst = perMinute
	}
	limiter := newIPRateLimiterInternal(rate.Every(time.Minute/time.Duration(perMinute)), burst)

	return func(c *gin.Context) {
		key := clientIPForRateLimitInternal(c)
		if !limiter.allow(key) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

// PerAgentTokenRateLimit returns a Gin middleware that limits requests per
// edge agent token to the given rate and burst. Requests without a token pass
// through so endpoint authentication can return the canonical auth error.
// This is a token-scoped limiter only; enrollment and tunnel routes must also
// stack PerIPRateLimit before it to provide IP-level back-pressure when tokens
// are missing, invalid, rotated, or stolen.
func PerAgentTokenRateLimit(perMinute int, burst int) gin.HandlerFunc {
	if perMinute <= 0 {
		perMinute = 10
	}
	if burst <= 0 {
		burst = perMinute
	}
	limiter := newIPRateLimiterInternal(rate.Every(time.Minute/time.Duration(perMinute)), burst)

	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader("X-Arcane-Agent-Token"))
		if key == "" {
			key = strings.TrimSpace(c.GetHeader("X-API-Key"))
		}
		if key == "" {
			c.Next()
			return
		}
		if !limiter.allow(agentTokenRateLimitKeyInternal(key)) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func agentTokenRateLimitKeyInternal(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func clientIPForRateLimitInternal(c *gin.Context) string {
	// Prefer the Gin-resolved client IP, which respects trusted proxy config.
	if ip := strings.TrimSpace(c.ClientIP()); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}
