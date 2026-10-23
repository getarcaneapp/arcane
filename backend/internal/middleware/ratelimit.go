package middleware

import (
	"cmp"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/pkg"
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

type limiterAge struct {
	key  string
	seen time.Time
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
	l.evictOldestEntriesInternal(max(len(l.limiters)-l.maxEntries+1, l.maxEntries/10), key)
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

	entries := make([]limiterAge, 0, len(l.limiters))
	for key, entry := range l.limiters {
		if key == protectedKey {
			continue
		}
		entries = append(entries, limiterAge{key: key, seen: entry.seen})
	}

	slices.SortFunc(entries, func(a, b limiterAge) int {
		return a.seen.Compare(b.seen)
	})

	for i := 0; i < count && i < len(entries); i++ {
		delete(l.limiters, entries[i].key)
	}
}

// PerIPRateLimit returns an Echo middleware that limits requests per client IP
// to the given rate and burst. It responds with 429 when the limit is exceeded.
func PerIPRateLimit(perMinute, burst int) echo.MiddlewareFunc {
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
func PerAgentTokenRateLimit(perMinute, burst int) echo.MiddlewareFunc {
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
			key := cmp.Or(strings.TrimSpace(req.Header.Get(HeaderAgentToken)), strings.TrimSpace(req.Header.Get(HeaderApiKey)))
			if key == "" {
				return next(c)
			}
			if !limiter.allow(kit.SHA256Hex(key)) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			}
			return next(c)
		}
	}
}

// PerTokenRateLimitForPaths gives known tokens independent budgets and limits others per IP.
// isKnown must be an in-memory lookup with bounded input, without request-driven cache fills.
// A nil lookup falls back to the shared IP limiter.
func PerTokenRateLimitForPaths(
	paths []string,
	perMinute int,
	burst int,
	tokenOf func(c *echo.Context) string,
	isKnown func(token string) bool,
) echo.MiddlewareFunc {
	if perMinute <= 0 {
		perMinute = 10
	}
	if burst <= 0 {
		burst = perMinute
	}
	ipLimiter := newIPRateLimiterInternal(rate.Every(time.Minute/time.Duration(perMinute)), burst)
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

			token := tokenOf(c)
			if token != "" && isKnown != nil && isKnown(token) {
				if !tokenLimiter.allow(kit.SHA256Hex(token)) {
					c.Response().Header().Set("Retry-After", "60")
					return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
				}
				return next(c)
			}

			if !ipLimiter.allow(clientIPForRateLimitInternal(c)) {
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
func PerIPRateLimitForPaths(paths []string, perMinute, burst int) echo.MiddlewareFunc {
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

func WarnUntrustedForwardedFor(trustedProxyNets []*net.IPNet) echo.MiddlewareFunc {
	var once sync.Once
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			if req.Header.Get("X-Forwarded-For") == "" {
				return next(c)
			}
			host, _, err := net.SplitHostPort(req.RemoteAddr)
			ip := net.ParseIP(kit.Ternary(err != nil, req.RemoteAddr, host))
			if ip == nil || !slices.ContainsFunc(trustedProxyNets, func(n *net.IPNet) bool { return n.Contains(ip) }) {
				once.Do(func() {
					slog.WarnContext(req.Context(),
						"X-Forwarded-For received from an untrusted peer; clients behind this proxy share one rate-limit bucket. Set ARCANE_TRUSTED_PROXIES to the proxy's address range.",
						"peer", req.RemoteAddr)
				})
			}
			return next(c)
		}
	}
}

func clientIPForRateLimitInternal(c *echo.Context) string {
	host := strings.TrimSpace(c.RealIP())
	if host == "" {
		var err error
		if host, _, err = net.SplitHostPort(c.Request().RemoteAddr); err != nil {
			host = c.Request().RemoteAddr
		}
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return host
}
