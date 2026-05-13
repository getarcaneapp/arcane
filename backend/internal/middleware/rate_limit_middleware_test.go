package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestPerIPRateLimit_AllowsUnderBurstAndBlocksOver(t *testing.T) {
	router := echo.New()
	router.POST("/t", func(c echo.Context) error {
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
	router.POST("/t", func(c echo.Context) error {
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
	router.POST("/t", func(c echo.Context) error {
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
	router.POST("/limited", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	router.POST("/unlimited", func(c echo.Context) error {
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

	for i := 0; i < 10; i++ {
		require.Equal(t, http.StatusOK, doReq("/unlimited"))
	}
}

func TestPerIPRateLimitForPaths_TracksDistinctIPs(t *testing.T) {
	router := echo.New()
	router.IPExtractor = echo.ExtractIPDirect()

	router.Use(PerIPRateLimitForPaths([]string{"/limited"}, 60, 1))
	router.POST("/limited", func(c echo.Context) error {
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
	router.POST("/a", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	router.POST("/b", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

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
	router.POST("/webhooks/trigger/:token", func(c echo.Context) error {
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
