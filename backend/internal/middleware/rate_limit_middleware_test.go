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

// newTokenRateLimitTestRouterInternal wires PerTokenRateLimitForPaths ahead
// of handler and returns a doReq helper posting from a fixed source IP.
func newTokenRateLimitTestRouterInternal(
	t *testing.T,
	perMinute, burst int,
	isAuthentic func(string) bool,
	handler echo.HandlerFunc,
) (doReq func(token string) int) {
	t.Helper()

	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()
	router.Use(PerTokenRateLimitForPaths([]string{"/webhooks/trigger/:token"}, perMinute, burst, isAuthentic))
	router.POST("/webhooks/trigger/:token", handler)

	return func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/trigger/"+token, nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
}

func okHandlerInternal(c *echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func TestPerTokenRateLimitForPaths_TracksDistinctTokensNotIP(t *testing.T) {
	acceptAll := func(string) bool { return true }
	doReq := newTokenRateLimitTestRouterInternal(t, 60, 1, acceptAll, okHandlerInternal)

	require.Equal(t, http.StatusOK, doReq("token-a"))
	require.Equal(t, http.StatusOK, doReq("token-b"))
	require.Equal(t, http.StatusOK, doReq("token-c"))
	require.Equal(t, http.StatusTooManyRequests, doReq("token-a"))
}

// TestPerTokenRateLimitForPaths_NilAuthenticatorFailsClosedToSharedIPBucket proves
// a nil isAuthentic does not hand out independent per-token buckets: all nonempty
// tokens contend for one shared IP bucket and trip 429 together.
func TestPerTokenRateLimitForPaths_NilAuthenticatorFailsClosedToSharedIPBucket(t *testing.T) {
	doReq := newTokenRateLimitTestRouterInternal(t, 60, 1, nil, okHandlerInternal)

	require.Equal(t, http.StatusOK, doReq("token-a"))
	require.Equal(t, http.StatusTooManyRequests, doReq("token-b"),
		"a nil authenticator must fail closed: distinct tokens share the IP bucket")
	require.Equal(t, http.StatusTooManyRequests, doReq("token-a"))
}

func TestPerTokenRateLimitForPaths_IPCeilingBoundsUnauthenticTokenCycling(t *testing.T) {
	rejectAll := func(string) bool { return false }
	const perMinute, burst = 1, 3
	handlerHits := 0
	doReq := newTokenRateLimitTestRouterInternal(t, perMinute, burst, rejectAll, func(c *echo.Context) error {
		handlerHits++
		return c.NoContent(http.StatusNotFound)
	})

	rejections := 0
	for i := range 200 {
		if doReq(strconv.Itoa(i)) == http.StatusTooManyRequests {
			rejections++
		}
	}

	require.Positive(t, rejections, "expected the IP ceiling to reject some requests once its burst is exhausted")
	require.Equal(t, burst, handlerHits,
		"distinct unauthenticated tokens from one IP must reach the handler only up to the configured burst; more would mean the old 10x IP allowance came back")
}

func TestPerTokenRateLimitForPaths_MalformedTokensGetNoBucketButStillHitIPCeiling(t *testing.T) {
	isAuthentic := func(token string) bool {
		return token == "well-formed"
	}
	const perMinute, burst = 2, 2

	doReq := newTokenRateLimitTestRouterInternal(t, perMinute, burst, isAuthentic, okHandlerInternal)

	successes := 0
	for i := range burst + 10 {
		if doReq("not-well-formed-"+strconv.Itoa(i)) == http.StatusOK {
			successes++
		}
	}

	require.LessOrEqual(t, successes, burst,
		"malformed tokens must be bounded by the IP ceiling, not given their own per-token bucket")
}

func TestPerTokenRateLimitForPaths_AuthenticTokenNotStarvedByGarbageFromSameIP(t *testing.T) {
	isAuthentic := func(token string) bool {
		return token == "authentic"
	}
	doReq := newTokenRateLimitTestRouterInternal(t, 2, 2, isAuthentic, okHandlerInternal)

	for i := range 100 {
		doReq("garbage-" + strconv.Itoa(i))
	}
	require.Equal(t, http.StatusTooManyRequests, doReq("garbage-final"),
		"IP ceiling must be exhausted by the garbage traffic")

	require.Equal(t, http.StatusOK, doReq("authentic"),
		"an authentic token must not be starved by garbage traffic from its source IP")
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
