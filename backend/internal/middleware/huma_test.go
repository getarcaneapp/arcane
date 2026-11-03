package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestEchoHandlerWritesRawResponse(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "raw",
		Method:      http.MethodPost,
		Path:        "/raw/{name}",
		Hidden:      true,
	}, EchoHandler(func(c *echo.Context) error {
		if err := c.Request().ParseForm(); err != nil {
			return err
		}
		return c.String(http.StatusTeapot, c.Param("name")+":"+c.Request().Form.Get("grant_type"))
	}))
	huma.Register(api, huma.Operation{
		OperationID: "failing",
		Method:      http.MethodGet,
		Path:        "/failing",
		Hidden:      true,
	}, EchoHandler(func(*echo.Context) error {
		return echo.ErrNotFound
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/raw/token", strings.NewReader("grant_type=exchange"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusTeapot, rec.Code)
	require.Equal(t, "token:exchange", rec.Body.String())
	require.Equal(t, "text/plain; charset=UTF-8", rec.Header().Get("Content-Type"))

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/failing", http.NoBody))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestEchoMiddlewareGatesOperation(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "limited",
		Method:      http.MethodPost,
		Path:        "/limited",
		Hidden:      true,
		Middlewares: huma.Middlewares{EchoMiddleware(PerIPRateLimit(1, 1))},
	}, EchoHandler(func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	}))

	serve := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/limited", http.NoBody))
		return rec
	}
	require.Equal(t, http.StatusNoContent, serve().Code)
	limited := serve()
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, "60", limited.Header().Get("Retry-After"))
}
