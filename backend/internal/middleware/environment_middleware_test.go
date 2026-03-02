package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/gin-gonic/gin"
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
		authValidator: func(ctx context.Context, c *gin.Context) bool {
			_ = ctx
			_ = c
			return true
		},
		httpClient: &http.Client{Timeout: proxyTimeout},
		registry:   edge.NewTunnelRegistry(),
	}
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
