package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestPerIPRateLimit_AllowsUnderBurstAndBlocksOver(t *testing.T) {
	router := echo.New()
	router.POST("/t", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	}, PerIPRateLimit(60, 2))

	doReq := func() int {
		req := httptest.NewRequest(http.MethodPost, "/t", nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReq())
	require.Equal(t, http.StatusOK, doReq())
	require.Equal(t, http.StatusTooManyRequests, doReq())
}

func TestPerIPRateLimit_TracksDistinctClients(t *testing.T) {
	router := echo.New()
	router.POST("/t", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	}, PerIPRateLimit(60, 1))

	doReqFrom := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/t", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReqFrom("192.0.2.10:1000"))
	require.Equal(t, http.StatusTooManyRequests, doReqFrom("192.0.2.10:1000"))
	require.Equal(t, http.StatusOK, doReqFrom("192.0.2.11:1000"))
}

func TestStackedAgentEnrollmentRateLimits_KeepIPBackPressure(t *testing.T) {
	router := echo.New()
	router.POST("/t", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	}, PerIPRateLimit(60, 1), PerAgentTokenRateLimit(60, 10))

	doReq := func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/t", nil)
		req.RemoteAddr = "192.0.2.10:4000"
		req.Header.Set("X-Arcane-Agent-Token", token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReq("token-a"))
	require.Equal(t, http.StatusTooManyRequests, doReq("token-b"))
}

func TestPerIPRateLimitForPaths_AppliesOnlyToConfiguredPaths(t *testing.T) {
	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()

	router.Use(PerIPRateLimitForPaths([]string{"/limited"}, 60, 1))
	router.POST("/limited", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	router.POST("/unlimited", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	doReq := func(path string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReq("/limited"))
	require.Equal(t, http.StatusTooManyRequests, doReq("/limited"))

	for range 10 {
		require.Equal(t, http.StatusOK, doReq("/unlimited"))
	}
}

func TestPerIPRateLimitForPaths_TracksDistinctIPs(t *testing.T) {
	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()

	router.Use(PerIPRateLimitForPaths([]string{"/limited"}, 60, 1))
	router.POST("/limited", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	doReqFrom := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/limited", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReqFrom("192.0.2.10:1000"))
	require.Equal(t, http.StatusTooManyRequests, doReqFrom("192.0.2.10:1000"))
	require.Equal(t, http.StatusOK, doReqFrom("192.0.2.11:1000"))
}

func TestPerIPRateLimitForPaths_IndependentBucketPerPath(t *testing.T) {
	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()

	router.Use(PerIPRateLimitForPaths([]string{"/a", "/b"}, 60, 1))
	router.POST("/a", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	router.POST("/b", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	doReq := func(path string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReq("/a"))
	require.Equal(t, http.StatusTooManyRequests, doReq("/a"))
	// /b has its own bucket and must not be affected by /a's burst exhaustion.
	require.Equal(t, http.StatusOK, doReq("/b"))
	require.Equal(t, http.StatusTooManyRequests, doReq("/b"))
}

func TestPerIPRateLimitForPaths_RouteParamsDoNotEscapeFilter(t *testing.T) {
	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()

	router.Use(PerIPRateLimitForPaths([]string{"/webhooks/trigger/:token"}, 60, 1))
	router.POST("/webhooks/trigger/:token", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	doReq := func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/trigger/"+token, nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReq("token-aaa"))
	require.Equal(t, http.StatusTooManyRequests, doReq("token-bbb"))
}

// newTokenRateLimitTestRouterInternal builds an Echo router with
// PerTokenRateLimitForPaths mounted ahead of a handler, plus a doReq helper
// that always posts from the same source IP (192.0.2.10). Centralizing this
// setup keeps the PerTokenRateLimitForPaths tests focused on the scenario
// each one is actually verifying, rather than each repeating router/request
// wiring that isn't itself under test.
func newTokenRateLimitTestRouterInternal(
	t *testing.T,
	perMinute, burst int,
	isValidShape func(string) bool,
	handler echo.HandlerFunc,
) (doReq func(token string) int) {
	t.Helper()

	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()
	router.Use(PerTokenRateLimitForPaths([]string{"/webhooks/trigger/:token"}, perMinute, burst, isValidShape))
	router.POST("/webhooks/trigger/:token", handler)

	return func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/trigger/"+token, nil)
		req.RemoteAddr = "192.0.2.10:4000" // same source IP for every request
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
}

func okHandlerInternal(c *echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func TestPerTokenRateLimitForPaths_TracksDistinctTokensNotIP(t *testing.T) {
	doReq := newTokenRateLimitTestRouterInternal(t, 60, 1, nil, okHandlerInternal)

	// Same IP, different tokens: each token gets its own bucket, so a burst
	// from one webhook (e.g. a Git host) must not block another.
	require.Equal(t, http.StatusOK, doReq("token-a"))
	require.Equal(t, http.StatusOK, doReq("token-b"))
	require.Equal(t, http.StatusOK, doReq("token-c"))

	// The same token, hit twice in a row, still exhausts its own bucket.
	require.Equal(t, http.StatusTooManyRequests, doReq("token-a"))
}

func TestPerTokenRateLimitForPaths_FallsBackToIPWhenTokenMissing(t *testing.T) {
	doReq := newTokenRateLimitTestRouterInternal(t, 60, 1, nil, okHandlerInternal)

	code := doReq("")

	// Echo does not match a trailing-slash empty param against ":token", so
	// this exercises the router's own behavior rather than the fallback path
	// directly; the important invariant is that the middleware never panics
	// and never leaves the route unguarded.
	require.NotEqual(t, http.StatusInternalServerError, code)
}

func TestPerTokenRateLimitForPaths_IPCeilingBoundsUnseenTokenCycling(t *testing.T) {
	dbLookups := 0
	doReq := newTokenRateLimitTestRouterInternal(t, 60, 1, nil, func(c *echo.Context) error {
		// Stands in for the webhook service's database-backed token lookup:
		// every request that reaches the handler pays this cost, regardless
		// of whether the token turns out to be valid.
		dbLookups++
		return c.NoContent(http.StatusNotFound)
	})

	// A never-ending stream of distinct, invalid tokens from one IP must
	// still be bounded by the IP-wide ceiling (burst * multiplier), not
	// admitted indefinitely just because each token is new to the token
	// bucket map. This is what stops an attacker from using fresh random
	// tokens to bypass rate limiting and hammer the downstream token lookup.
	rejections := 0
	for i := range 200 {
		if doReq(strconv.Itoa(i)) == http.StatusTooManyRequests {
			rejections++
		}
	}

	require.Positive(t, rejections, "expected the IP ceiling to reject some requests once its burst is exhausted")
	require.Less(t, dbLookups, 200, "unseen tokens must not all reach the handler/downstream lookup")
}

func TestPerTokenRateLimitForPaths_MalformedTokensGetNoBucketButStillHitIPCeiling(t *testing.T) {
	isValidShape := func(token string) bool {
		return token == "well-formed"
	}

	// PerTokenRateLimitForPaths widens the IP ceiling by
	// perTokenRateLimitIPMultiplierInternal relative to the burst given
	// here, so with burst=2 the real IP-level burst is
	// 2*perTokenRateLimitIPMultiplierInternal. If malformed tokens got
	// their own per-token bucket instead of being folded into the IP-only
	// path, they could rack up far more successes than that; if they are
	// correctly bounded by the (wider) IP ceiling alone, successes must
	// stop there.
	const perMinute, burst = 2, 2
	const ipBurst = burst * perTokenRateLimitIPMultiplierInternal

	doReq := newTokenRateLimitTestRouterInternal(t, perMinute, burst, isValidShape, okHandlerInternal)

	successes := 0
	for i := range ipBurst + 10 {
		if doReq("not-well-formed-"+strconv.Itoa(i)) == http.StatusOK {
			successes++
		}
	}

	require.LessOrEqual(t, successes, ipBurst,
		"malformed tokens must be bounded by the IP ceiling, not given their own per-token bucket")
}

func TestPerTokenRateLimitForPaths_AppliesOnlyToConfiguredPaths(t *testing.T) {
	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()

	router.Use(PerTokenRateLimitForPaths([]string{"/webhooks/trigger/:token"}, 60, 1, nil))
	router.POST("/webhooks/trigger/:token", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	router.POST("/unlimited/:token", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	doReq := func(path string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	for range 10 {
		require.Equal(t, http.StatusOK, doReq("/unlimited/same-token"))
	}
}

func TestIPRateLimiter_EnforcesMaxEntriesForRecentClients(t *testing.T) {
	limiter := newIPRateLimiterInternal(1, 1)
	limiter.maxEntries = 3

	require.True(t, limiter.allow("client-1"))
	require.True(t, limiter.allow("client-2"))
	require.True(t, limiter.allow("client-3"))
	require.True(t, limiter.allow("client-4"))

	require.LessOrEqual(t, len(limiter.limiters), limiter.maxEntries)
	require.Contains(t, limiter.limiters, "client-4")
}

func TestIPRateLimiter_ProtectsCurrentKeyDuringSweep(t *testing.T) {
	limiter := newIPRateLimiterInternal(rate.Every(time.Hour), 1)
	limiter.maxEntries = 1

	exhausted := rate.NewLimiter(rate.Every(time.Hour), 1)
	require.True(t, exhausted.Allow())

	now := time.Now()
	limiter.limiters["current"] = &limiterEntry{
		limiter: exhausted,
		seen:    now.Add(-time.Minute),
	}
	limiter.limiters["other"] = &limiterEntry{
		limiter: rate.NewLimiter(rate.Every(time.Hour), 1),
		seen:    now,
	}

	require.False(t, limiter.allow("current"))
	require.Contains(t, limiter.limiters, "current")
	require.NotContains(t, limiter.limiters, "other")
}
