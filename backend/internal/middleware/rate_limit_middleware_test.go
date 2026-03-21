package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestPerIPRateLimit_AllowsUnderBurstAndBlocksOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/t", PerIPRateLimit(60, 2), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	doReq := func() int {
		req := httptest.NewRequest(http.MethodPost, "/t", nil)
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// Burst of 2 must succeed, the third immediate request must be throttled.
	require.Equal(t, http.StatusOK, doReq())
	require.Equal(t, http.StatusOK, doReq())
	require.Equal(t, http.StatusTooManyRequests, doReq())
}

func TestPerIPRateLimit_TracksDistinctClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/t", PerIPRateLimit(60, 1), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	doReqFrom := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/t", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, doReqFrom("192.0.2.10:1000"))
	require.Equal(t, http.StatusTooManyRequests, doReqFrom("192.0.2.10:1000"))
	// Distinct client must not be affected by the first client's throttling.
	require.Equal(t, http.StatusOK, doReqFrom("192.0.2.11:1000"))
}

func TestStackedAgentEnrollmentRateLimits_KeepIPBackPressure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/t", PerIPRateLimit(60, 1), PerAgentTokenRateLimit(60, 10), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

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
