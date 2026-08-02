//go:build !exclude_frontend

package frontend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// TestRegisterFrontend_ServesSPA verifies the RouteNotFound fallback serves
// index.html for both the root path and unmatched client-side routes, while
// registered routes stay untouched. Regression test for the echo v5
// migration, where the router's not-found sentinel stopped matching
// *echo.HTTPError and every page load returned a 500.
func TestRegisterFrontend_ServesSPA(t *testing.T) {
	e := echo.New()
	e.GET("/api/health", func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	require.NoError(t, RegisterFrontend(e))

	for _, path := range []string{"/", "/containers/some-id"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, "path %s", path)
		require.Contains(t, rec.Body.String(), "<!doctype html", "path %s should serve index.html", path)
	}

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, strings.HasPrefix(rec.Body.String(), "ok"))
}
