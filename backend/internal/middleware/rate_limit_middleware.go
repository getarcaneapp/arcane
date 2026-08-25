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

	"github.com/labstack/echo/v5"
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

// PerIPRateLimit returns an Echo middleware that limits requests per client IP
// to the given rate and burst. It responds with 429 when the limit is exceeded.
func PerIPRateLimit(perMinute int, burst int) echo.MiddlewareFunc {
	if perMinute <= 0 {
		perMinute = 10
	}
	if burst <= 0 {
		burst = perMinute
	}
	limiter := newIPRateLimiterInternal(rate.Every(time.Minute/time.Duration(perMinute)), burst)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			key := clientIPForRateLimitInternal(c)
			if !limiter.allow(key) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}
			return next(c)
		}
	}
}

// PerAgentTokenRateLimit returns an Echo middleware that limits requests per
// edge agent token to the given rate and burst.
func PerAgentTokenRateLimit(perMinute int, burst int) echo.MiddlewareFunc {
	if perMinute <= 0 {
		perMinute = 10
	}
	if burst <= 0 {
		burst = perMinute
	}
	limiter := newIPRateLimiterInternal(rate.Every(time.Minute/time.Duration(perMinute)), burst)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			key := strings.TrimSpace(req.Header.Get("X-Arcane-Agent-Token"))
			if key == "" {
				key = strings.TrimSpace(req.Header.Get("X-Api-Key"))
			}
			if key == "" {
				return next(c)
			}
			if !limiter.allow(agentTokenRateLimitKeyInternal(key)) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}
			return next(c)
		}
	}
}

func agentTokenRateLimitKeyInternal(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// perTokenRateLimitIPMultiplierInternal widens the IP-level ceiling relative
// to the per-token ceiling in PerTokenRateLimitForPaths. A single IP
// legitimately fans out to many distinct, valid tokens (e.g. a Git host
// triggering several webhooks in quick succession), so the IP bucket must
// absorb that fan-out; it only needs to stay tight enough to bound the cost
// of an attacker cycling through invalid tokens, not to match the per-token
// rate one-for-one.
const perTokenRateLimitIPMultiplierInternal = 10

// PerTokenRateLimitForPaths returns an Echo middleware that rate-limits by
// BOTH client IP and the ":token" route param, for paths in the paths list
// (matched against c.Path(), the registered route pattern).
//
// The IP limit is checked first and always applies, at a wider ceiling
// (perTokenRateLimitIPMultiplierInternal times perMinute/burst): it is what
// bounds an unauthenticated caller from cycling through arbitrary invalid
// tokens to force repeated downstream lookups (e.g. a database-backed token
// check), since a fresh, never-seen token would otherwise always be admitted
// before that lookup happens. Only requests that pass the IP limit, carry a
// non-empty token, and pass isValidShape (if provided) are additionally
// checked against a bucket keyed by the token itself (SHA-256 hashed), at
// the exact perMinute/burst given. That second, tighter layer is what stops
// one busy, legitimate webhook from starving a different webhook's budget
// when both are triggered from the same source IP (e.g. a shared Git host)
// — it isolates buckets between tokens without ever exempting an
// unrecognized token from the IP-wide ceiling.
//
// isValidShape lets the caller reject tokens that are structurally
// malformed (wrong prefix/length/alphabet) before a per-token bucket is
// ever allocated for them, without this package needing to know the
// concrete token format. A malformed token never reaches the wrapped
// handler; the IP ceiling above is still the only thing standing between
// an attacker and this middleware for such tokens, matching the behavior
// for a request with no token param at all. isValidShape may be nil, in
// which case every non-empty token gets its own bucket (previous
// behavior). It must be cheap and side-effect free: no I/O, no locking.
func PerTokenRateLimitForPaths(paths []string, perMinute int, burst int, isValidShape func(string) bool) echo.MiddlewareFunc {
	if perMinute <= 0 {
		perMinute = 10
	}
	if burst <= 0 {
		burst = perMinute
	}
	ipLimiter := newIPRateLimiterInternal(
		rate.Every(time.Minute/time.Duration(perMinute*perTokenRateLimitIPMultiplierInternal)),
		burst*perTokenRateLimitIPMultiplierInternal,
	)
	tokenLimiter := newIPRateLimiterInternal(rate.Every(time.Minute/time.Duration(perMinute)), burst)

	pathSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		pathSet[p] = struct{}{}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if _, ok := pathSet[c.Path()]; !ok {
				return next(c)
			}

			// IP ceiling first, always: this is what bounds an unauthenticated
			// caller cycling through arbitrary invalid tokens, since a fresh,
			// never-seen token would otherwise always be admitted below before
			// the caller (the webhook service) gets a chance to reject it.
			if !ipLimiter.allow(clientIPForRateLimitInternal(c)) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}

			token := strings.TrimSpace(c.Param("token"))
			if token == "" || (isValidShape != nil && !isValidShape(token)) {
				return next(c)
			}

			// Second layer, only reached once the IP ceiling is satisfied:
			// isolates buckets between distinct tokens so a burst on one
			// webhook cannot deplete the budget for another webhook sharing
			// the same source IP (e.g. a shared Git host).
			if !tokenLimiter.allow(agentTokenRateLimitKeyInternal(token)) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}
			return next(c)
		}
	}
}

// PerIPRateLimitForPaths returns an Echo middleware that applies a per-IP
// rate limit only when c.Path() (the registered route pattern) is in paths.
// Each path gets its own independent token bucket, so traffic on one path
// does not deplete the budget for another (e.g. a login burst will not
// block a concurrent token refresh).
func PerIPRateLimitForPaths(paths []string, perMinute int, burst int) echo.MiddlewareFunc {
	limiters := make(map[string]echo.MiddlewareFunc, len(paths))
	for _, p := range paths {
		limiters[p] = PerIPRateLimit(perMinute, burst)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		gatedByPath := make(map[string]echo.HandlerFunc, len(limiters))
		for p, rl := range limiters {
			gatedByPath[p] = rl(next)
		}
		return func(c *echo.Context) error {
			gated, ok := gatedByPath[c.Path()]
			if !ok {
				return next(c)
			}
			return gated(c)
		}
	}
}

func clientIPForRateLimitInternal(c *echo.Context) string {
	if ip := strings.TrimSpace(c.RealIP()); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		return c.Request().RemoteAddr
	}
	return host
}
