package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func newTestEnvironmentMiddleware() *EnvironmentMiddleware {
	return &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		resolver: func(ctx context.Context, id string) (string, *string, bool, error) {
			_ = ctx
			return "edge://oracle-1", nil, true, nil
		},
		authValidator: func(ctx context.Context, c echo.Context) bool {
			_ = ctx
			_ = c
			return true
		},
		httpClient: &http.Client{Timeout: proxyTimeout},
		registry:   edge.NewTunnelRegistry(),
	}
}

func attachMiddleware(router *echo.Echo, mw *EnvironmentMiddleware) *echo.Group {
	api := router.Group("/api")
	api.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return mw.Handle(c, next)
		}
	})
	return api
}

func TestEnvironmentMiddleware_ReturnsBadGatewayForEdgeResourcesWithoutTunnel(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/containers", func(c echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/containers", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ProxiesDashboardResourcesForRemoteEnvironments(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/dashboard", func(c echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/dashboard", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsEdgeManagementEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/settings", func(c echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsEdgeMTLSDownloadEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/deployment/mtls/bundle", func(c echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/deployment/mtls/bundle", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsNotificationEndpointsLocal(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	router := echo.New()
	api := attachMiddleware(router, middleware)

	localHandlerHit := false
	api.GET("/environments/:id/notifications/settings", func(c echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/notifications/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ProxyWebSocketRejectsEdgeTargetsWithoutTunnel(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	e := echo.New()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/ws/system/stats", nil)
	c := e.NewContext(req, recorder)

	_ = middleware.proxyWebSocket(c, "edge://oracle-1/api/environments/0/ws/system/stats", nil, "env-edge")

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
}

func TestEnvironmentMiddleware_ProxyHTTPRejectsEdgeTargetsWithoutTunnel(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	e := echo.New()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/containers", nil)
	c := e.NewContext(req, recorder)

	_ = middleware.proxyHTTP(c, "edge://oracle-1/api/environments/0/containers", nil)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
}

func TestIsWebSocketUpgrade(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()

	tests := []struct {
		name     string
		headers  map[string]string
		expected bool
	}{
		{
			name:     "valid websocket upgrade",
			headers:  map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Sec-Websocket-Key": "dGhlIHNhbXBsZSBub25jZQ==", "Sec-Websocket-Version": "13"},
			expected: true,
		},
		{
			name:     "normal GET request",
			headers:  map[string]string{},
			expected: false,
		},
		{
			name:     "only upgrade header from reverse proxy",
			headers:  map[string]string{"Upgrade": "websocket"},
			expected: false,
		},
		{
			name:     "only connection upgrade from reverse proxy",
			headers:  map[string]string{"Connection": "Upgrade"},
			expected: false,
		},
		{
			name:     "connection upgrade with keep-alive from nginx",
			headers:  map[string]string{"Connection": "upgrade, keep-alive"},
			expected: false,
		},
		{
			name:     "only sec-websocket-key leaked by proxy",
			headers:  map[string]string{"Sec-Websocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="},
			expected: false,
		},
		{
			name:     "upgrade and connection but no sec-websocket-key",
			headers:  map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/environments/env-1/containers", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			c := e.NewContext(req, recorder)

			result := middleware.isWebSocketUpgrade(c)
			assert.Equal(t, tt.expected, result, "headers: %v", tt.headers)
		})
	}
}

func TestEnvironmentMiddleware_CreateProxyRequest_RejectsInvalidProxyTarget(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	e := echo.New()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/containers", nil)
	c := e.NewContext(req, recorder)

	_, err := middleware.createProxyRequest(c, "ftp://example.com/containers", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid proxy target URL")
}
