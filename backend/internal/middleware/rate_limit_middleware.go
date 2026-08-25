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

// perTokenRateLimitIPMultiplierInternal widens the IP ceiling relative to
// the per-token ceiling, since one IP can legitimately trigger many valid
// tokens in a burst (e.g. a Git host firing several webhooks at once).
const perTokenRateLimitIPMultiplierInternal = 10

// knownValidTokenTTLInternal is how long a token that has already produced
// a non-404 response is exempted from the shared IP ceiling. This keeps a
// busy attacker cycling through invalid tokens from starving a legitimate,
// already-confirmed webhook sharing the same source IP.
const knownValidTokenTTLInternal = 10 * time.Minute

// knownTokenCache tracks tokens that have already been confirmed to exist
// (any response other than 404), so repeat requests for them can bypass the
// shared IP ceiling and go straight to their own per-token bucket.
type knownTokenCache struct {
	mu      sync.Mutex
	seenAt  map[string]time.Time
	ttl     time.Duration
	maxSize int
}

func newKnownTokenCacheInternal(ttl time.Duration) *knownTokenCache {
	return &knownTokenCache{seenAt: make(map[string]time.Time), ttl: ttl, maxSize: 10000}
}

func (c *knownTokenCache) markValid(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.seenAt) >= c.maxSize {
		return // full: skip caching rather than grow unbounded
	}
	c.seenAt[key] = time.Now()
}

func (c *knownTokenCache) isKnownValid(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen, ok := c.seenAt[key]
	if !ok {
		return false
	}
	if time.Since(seen) > c.ttl {
		delete(c.seenAt, key)
		return false
	}
	return true
}

// PerTokenRateLimitForPaths rate-limits by client IP first (at
// perTokenRateLimitIPMultiplierInternal times perMinute/burst), then by the
// ":token" route param, for paths in the paths list (matched against
// c.Path()). Once isValidResponse confirms a token via a successful
// request, that token is cached and bypasses the IP ceiling on later
// requests, so an attacker cycling through invalid tokens on a shared IP
// cannot starve a legitimate, already-confirmed webhook. isValidShape, if
// given, rejects malformed tokens before they get a bucket; both callbacks
// must be cheap and side-effect free.
func PerTokenRateLimitForPaths(
	paths []string,
	perMinute int,
	burst int,
	isValidShape func(token string) bool,
	isValidResponse func(status int) bool,
) echo.MiddlewareFunc {
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
	knownTokens := newKnownTokenCacheInternal(knownValidTokenTTLInternal)

	pathSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		pathSet[p] = struct{}{}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if _, ok := pathSet[c.Path()]; !ok {
				return next(c)
			}

			token := strings.TrimSpace(c.Param("token"))
			wellFormed := token != "" && (isValidShape == nil || isValidShape(token))
			tokenKey := ""
			if wellFormed {
				tokenKey = agentTokenRateLimitKeyInternal(token)
			}

			// A token already confirmed to exist skips the shared IP
			// ceiling and goes straight to its own bucket, so it can't be
			// starved by unrelated traffic on the same IP.
			if wellFormed && knownTokens.isKnownValid(tokenKey) {
				if !tokenLimiter.allow(tokenKey) {
					c.Response().Header().Set("Retry-After", "60")
					return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
				}
				return next(c)
			}

			if !ipLimiter.allow(clientIPForRateLimitInternal(c)) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}

			if !wellFormed {
				return next(c)
			}

			if !tokenLimiter.allow(tokenKey) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}

			err := next(c)
			if err == nil && isValidResponse != nil {
				if resp, ok := c.Response().(*echo.Response); ok && isValidResponse(resp.Status) {
					knownTokens.markValid(tokenKey)
				}
			}
			return err
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
