package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHostlessAutoPatchNoRouteHandler_RewritesGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(NewHostlessAutoPatchNoRouteHandler(router))
	router.GET("/api/environments/:id/settings", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req, err := http.NewRequest(http.MethodGet, "/environments/0/settings", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Host = ""
	req.URL.Host = ""
	req.RequestURI = req.URL.RequestURI()

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, resp.Code)
	}
}

func TestHostlessAutoPatchNoRouteHandler_RewritesPut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(NewHostlessAutoPatchNoRouteHandler(router))
	router.PUT("/api/users/:id", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req, err := http.NewRequest(http.MethodPut, "/users/123", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Host = ""
	req.URL.Host = ""
	req.RequestURI = req.URL.RequestURI()

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, resp.Code)
	}
}

func TestHostlessAutoPatchNoRouteHandler_DoesNotRewriteHostedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(NewHostlessAutoPatchNoRouteHandler(router))
	router.GET("/api/environments/:id/settings", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/environments/0/settings", nil)
	req.Host = "localhost:3552"

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
}

func TestHostlessAutoPatchNoRouteHandler_DoesNotRewritePatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(NewHostlessAutoPatchNoRouteHandler(router))
	router.PATCH("/api/environments/:id/settings", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req, err := http.NewRequest(http.MethodPatch, "/environments/0/settings", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Host = ""
	req.URL.Host = ""
	req.RequestURI = req.URL.RequestURI()

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
}
