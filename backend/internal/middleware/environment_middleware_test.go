package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingReadCloser struct {
	reader io.Reader
	reads  int
	closed bool
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func newTestEnvironmentMiddleware() *EnvironmentMiddleware {
	return &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		resolver: func(ctx context.Context, id string) (string, *string, bool, error) {
			_ = ctx
			return "edge://oracle-1", nil, true, nil
		},
		authValidator: func(ctx context.Context, c echo.Context) (*authz.PermissionSet, bool) {
			_ = ctx
			_ = c
			return authz.SudoPermissionSet(), true
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

func TestEnvironmentMiddleware_KeepsWebhookEndpointsLocal(t *testing.T) {
	tests := []struct {
		name   string
		method string
		route  string
		path   string
	}{
		{
			name:   "list webhooks",
			method: http.MethodGet,
			route:  "/environments/:id/webhooks",
			path:   "/api/environments/env-edge/webhooks",
		},
		{
			name:   "delete webhook",
			method: http.MethodDelete,
			route:  "/environments/:id/webhooks/:webhookId",
			path:   "/api/environments/env-edge/webhooks/wh-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := newTestEnvironmentMiddleware()
			router := echo.New()
			api := attachMiddleware(router, middleware)

			localHandlerHit := false
			api.Add(tt.method, tt.route, func(c echo.Context) error {
				localHandlerHit = true
				return c.JSON(http.StatusOK, map[string]any{"success": true})
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "\"success\":true")
			assert.True(t, localHandlerHit)
		})
	}
}

func TestEnvironmentMiddleware_KeepsActivityEndpointsLocal(t *testing.T) {
	tests := []struct {
		name   string
		method string
		route  string
		path   string
	}{
		{
			name:   "list activities",
			method: http.MethodGet,
			route:  "/environments/:id/activities",
			path:   "/api/environments/env-edge/activities?limit=50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := newTestEnvironmentMiddleware()
			router := echo.New()
			api := attachMiddleware(router, middleware)

			localHandlerHit := false
			api.Add(tt.method, tt.route, func(c echo.Context) error {
				localHandlerHit = true
				return c.JSON(http.StatusOK, map[string]any{"success": true})
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "\"success\":true")
			assert.True(t, localHandlerHit)
		})
	}
}

func TestEnvironmentMiddleware_LocalEnvironmentSkipsProxyPermissionCheck(t *testing.T) {
	// The local environment ("0") is served directly and is never proxied, so
	// the proxy's per-environment authorization must not apply to it. Set up a
	// matcher that would require containers:list and a caller with no
	// permissions at all: if local requests were subject to proxy authz, this
	// would be a 403. It must instead fall through to the local handler, where
	// the operation's own RequirePermission middleware enforces access.
	matcher := authz.NewPermissionMatcher()
	matcher.Add(http.MethodGet, "/containers", authz.PermContainersList)

	mw := &EnvironmentMiddleware{
		localID:   "0",
		paramName: "id",
		matcher:   matcher,
		authValidator: func(ctx context.Context, c echo.Context) (*authz.PermissionSet, bool) {
			_ = ctx
			_ = c
			return authz.NewPermissionSet(), true
		},
		httpClient: &http.Client{Timeout: proxyTimeout},
		registry:   edge.NewTunnelRegistry(),
	}

	router := echo.New()
	api := attachMiddleware(router, mw)

	localHandlerHit := false
	api.GET("/environments/:id/containers", func(c echo.Context) error {
		localHandlerHit = true
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/environments/0/containers", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, localHandlerHit, "local environment request must reach the local handler, not be proxy-authorized")
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

func TestEnvironmentMiddleware_CreateProxyRequest_DoesNotReadBody(t *testing.T) {
	middleware := newTestEnvironmentMiddleware()
	e := echo.New()
	recorder := httptest.NewRecorder()
	body := &trackingReadCloser{reader: strings.NewReader("streamed request body")}
	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-direct/projects", body)
	req.ContentLength = 21
	req.TransferEncoding = []string{"chunked"}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("streamed request body")), nil
	}
	c := e.NewContext(req, recorder)

	proxyReq, err := middleware.createProxyRequest(c, "http://remote.example/projects", nil)
	require.NoError(t, err)

	assert.Same(t, body, proxyReq.Body)
	assert.Zero(t, body.reads)
	assert.False(t, body.closed)
	assert.Equal(t, req.ContentLength, proxyReq.ContentLength)
	assert.Equal(t, req.TransferEncoding, proxyReq.TransferEncoding)
	require.NotNil(t, proxyReq.GetBody)
	replay, err := proxyReq.GetBody()
	require.NoError(t, err)
	t.Cleanup(func() { _ = replay.Close() })
	replayedBody, err := io.ReadAll(replay)
	require.NoError(t, err)
	assert.Equal(t, "streamed request body", string(replayedBody))
	assert.Zero(t, body.reads)
}

func TestEnvironmentMiddleware_ProxyHTTP_StreamsLargeChunkedBody(t *testing.T) {
	const chunkCount = 256
	chunk := bytes.Repeat([]byte("arcane-stream-"), 2560)
	expectedHash := sha256.New()
	for range chunkCount {
		_, err := expectedHash.Write(chunk)
		require.NoError(t, err)
	}

	type uploadResult struct {
		bytesRead        int64
		hash             [sha256.Size]byte
		contentLength    int64
		transferEncoding []string
		contentType      string
		err              error
	}
	resultCh := make(chan uploadResult, 1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := sha256.New()
		bytesRead, err := io.Copy(hash, r.Body)
		resultCh <- uploadResult{
			bytesRead:        bytesRead,
			hash:             [sha256.Size]byte(hash.Sum(nil)),
			contentLength:    r.ContentLength,
			transferEncoding: r.TransferEncoding,
			contentType:      r.Header.Get("Content-Type"),
			err:              err,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(remote.Close)

	pipeReader, pipeWriter := io.Pipe()
	writeErrCh := make(chan error, 1)
	go func() {
		var writeErr error
		for range chunkCount {
			if _, writeErr = pipeWriter.Write(chunk); writeErr != nil {
				break
			}
		}
		closeErr := pipeWriter.CloseWithError(writeErr)
		writeErrCh <- closeErr
	}()

	middleware := newTestEnvironmentMiddleware()
	middleware.httpClient = remote.Client()
	e := echo.New()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-direct/upload", pipeReader)
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Content-Type", "application/octet-stream")
	c := e.NewContext(req, recorder)

	require.NoError(t, middleware.proxyHTTP(c, remote.URL+"/upload", nil))
	require.NoError(t, <-writeErrCh)
	result := <-resultCh
	require.NoError(t, result.err)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, int64(len(chunk)*chunkCount), result.bytesRead)
	assert.Equal(t, [sha256.Size]byte(expectedHash.Sum(nil)), result.hash)
	assert.Equal(t, int64(-1), result.contentLength)
	assert.Equal(t, []string{"chunked"}, result.transferEncoding)
	assert.Equal(t, "application/octet-stream", result.contentType)
}
