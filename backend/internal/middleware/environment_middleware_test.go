package middleware

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEnvironmentMiddleware() *EnvironmentMiddleware {
	return newTestEnvironmentMiddlewareWithResolver(func(ctx context.Context, id string) (string, *string, bool, error) {
		_ = ctx
		return "edge://oracle-1", nil, true, nil
	})
}

func newTestEnvironmentMiddlewareWithResolver(resolver EnvResolver) *EnvironmentMiddleware {
	return &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		resolver:  resolver,
		authValidator: func(ctx context.Context, c *gin.Context) bool {
			_ = ctx
			_ = c
			return true
		},
		httpClient: &http.Client{Timeout: proxyTimeout},
		registry:   edge.NewTunnelRegistry(),
	}
}

func TestEnvironmentMiddleware_ProxyHTTPStreamsRequestBodyToRemote(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var (
		receivedBody          string
		receivedPath          string
		receivedContentType   string
		receivedContentLength int64
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		receivedBody = string(body)
		receivedPath = r.URL.Path
		receivedContentType = r.Header.Get("Content-Type")
		receivedContentLength = r.ContentLength

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"proxied":true}`))
	}))
	defer upstream.Close()

	middleware := newTestEnvironmentMiddlewareWithResolver(func(ctx context.Context, id string) (string, *string, bool, error) {
		_ = ctx
		_ = id
		return upstream.URL, nil, true, nil
	})

	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.Handle)

	localHandlerHit := false
	api.POST("/environments/:id/projects", func(c *gin.Context) {
		localHandlerHit = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	requestBody := `{"name":"remote project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-remote/projects?from=test", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, `{"proxied":true}`, recorder.Body.String())
	assert.Equal(t, "/api/environments/0/projects", receivedPath)
	assert.Equal(t, "application/json", receivedContentType)
	assert.Equal(t, int64(len(requestBody)), receivedContentLength)
	assert.Equal(t, requestBody, receivedBody)
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_CreateProxyRequestDoesNotLogRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logBuffer bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(originalLogger)

	middleware := newTestEnvironmentMiddleware()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	secretBody := "super-secret-payload"
	c.Request = httptest.NewRequest(http.MethodPost, "/api/environments/env-remote/projects", strings.NewReader(secretBody))
	c.Request.Header.Set("Content-Type", "application/json")

	req, err := middleware.createProxyRequest(c, "https://example.com/api/environments/0/projects", nil)
	require.NoError(t, err)
	require.NotNil(t, req)

	output := logBuffer.String()
	assert.Contains(t, output, "Creating proxy request")
	assert.NotContains(t, output, secretBody)
	assert.NotContains(t, output, " body=")
}

func TestEnvironmentMiddleware_ReturnsBadGatewayForEdgeResourcesWithoutTunnel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	middleware := newTestEnvironmentMiddleware()
	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.Handle)

	localHandlerHit := false
	api.GET("/environments/:id/containers", func(c *gin.Context) {
		localHandlerHit = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/containers", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ProxiesDashboardResourcesForRemoteEnvironments(t *testing.T) {
	gin.SetMode(gin.TestMode)

	middleware := newTestEnvironmentMiddleware()
	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.Handle)

	localHandlerHit := false
	api.GET("/environments/:id/dashboard", func(c *gin.Context) {
		localHandlerHit = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/dashboard", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
	assert.False(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsEdgeManagementEndpointsLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	middleware := newTestEnvironmentMiddleware()
	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.Handle)

	localHandlerHit := false
	api.GET("/environments/:id/settings", func(c *gin.Context) {
		localHandlerHit = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_KeepsNotificationEndpointsLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	middleware := newTestEnvironmentMiddleware()
	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.Handle)

	localHandlerHit := false
	api.GET("/environments/:id/notifications/settings", func(c *gin.Context) {
		localHandlerHit = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/notifications/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "\"success\":true")
	assert.True(t, localHandlerHit)
}

func TestEnvironmentMiddleware_ProxyWebSocketRejectsEdgeTargetsWithoutTunnel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	middleware := newTestEnvironmentMiddleware()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/ws/system/stats", nil)

	middleware.proxyWebSocket(c, "edge://oracle-1/api/environments/0/ws/system/stats", nil, "env-edge")

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Edge agent is not connected")
}

func TestEnvironmentMiddleware_ProxyHTTPRejectsEdgeTargetsWithoutTunnel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	middleware := newTestEnvironmentMiddleware()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/environments/env-edge/containers", nil)

	middleware.proxyHTTP(c, "edge://oracle-1/api/environments/0/containers", nil)

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
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodGet, "/api/environments/env-1/containers", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			c.Request = req

			result := middleware.isWebSocketUpgrade(c)
			assert.Equal(t, tt.expected, result, "headers: %v", tt.headers)
		})
	}
}
