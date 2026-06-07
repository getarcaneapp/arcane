package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func newCSRFTestRouterInternal() *echo.Echo {
	cfg := &config.Config{Environment: "production", AppUrl: "https://arcane.example.com"}
	router := echo.New()
	router.Use(NewCSRFMiddleware(cfg).Add())
	router.POST("/api/test", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	router.POST("/api/webhooks/trigger/:token", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	return router
}

func TestCSRF_BlocksCrossSiteStateChange(t *testing.T) {
	router := newCSRFTestRouterInternal()
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCSRF_AllowsSameOrigin(t *testing.T) {
	router := newCSRFTestRouterInternal()
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

// Non-browser clients (curl, CLI, CI, the report's Python PoC) send no Origin or
// Sec-Fetch-Site header and must not be blocked.
func TestCSRF_AllowsNonBrowserClient(t *testing.T) {
	router := newCSRFTestRouterInternal()
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

// A header credential cannot be forged cross-origin by a browser, so even a
// cross-site Origin must not block such a request.
func TestCSRF_SkipsHeaderCredentialedRequests(t *testing.T) {
	router := newCSRFTestRouterInternal()
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("X-Api-Key", "some-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

// Public webhook trigger authenticates via its path token, not the session cookie,
// and may receive an Origin header from an external caller; it must be bypassed.
func TestCSRF_AllowsPublicBypassPaths(t *testing.T) {
	router := newCSRFTestRouterInternal()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/trigger/abc", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://ci.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
