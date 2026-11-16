package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

func TestCompressionMiddleware(t *testing.T) {
	large := `{"message":"` + strings.Repeat("arcane", 512) + `"}`
	tests := []struct {
		name        string
		path        string
		contentType string
		body        string
		acceptGzip  bool
		fanOut      bool
		websocket   bool
		compressed  bool
	}{
		{name: "large JSON", path: "/api/data", contentType: "application/json", body: large, acceptGzip: true, compressed: true},
		{name: "no gzip accepted", path: "/api/data", contentType: "application/json", body: large},
		{name: "small JSON", path: "/api/data", contentType: "application/json", body: `{}`, acceptGzip: true},
		{name: "event stream", path: "/api/events", contentType: "text/event-stream", body: large, acceptGzip: true},
		{name: "fan-out child", path: "/api/data", contentType: "application/json", body: large, acceptGzip: true, fanOut: true},
		{name: "websocket upgrade", path: "/api/data", contentType: "application/json", body: large, acceptGzip: true, websocket: true},
		{name: "stream path", path: "/api/stream", contentType: "application/json", body: large, acceptGzip: true},
		{name: "plain text", path: "/api/data", contentType: "text/plain", body: large, acceptGzip: true, compressed: true},
		{name: "problem JSON", path: "/api/data", contentType: "application/problem+json", body: large, acceptGzip: true, compressed: true},
		{name: "/api/ws/stats", path: "/api/ws/stats", contentType: "application/json", body: large, acceptGzip: true},
		{name: "/api/containers/a/logs", path: "/api/containers/a/logs", contentType: "application/json", body: large, acceptGzip: true},
		{name: "/api/logs/a", path: "/api/logs/a", contentType: "application/json", body: large, acceptGzip: true},
		{name: "/api/containers/a/terminal", path: "/api/containers/a/terminal", contentType: "application/json", body: large, acceptGzip: true},
		{name: "/api/terminal/a", path: "/api/terminal/a", contentType: "application/json", body: large, acceptGzip: true},
		{name: "outer aggregate", path: "/api/agents/a,b/containers/counts", contentType: "application/json", body: large, acceptGzip: true, compressed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := echo.New()
			group := router.Group("/api")
			group.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if tt.fanOut {
						c.SetRequest(c.Request().WithContext(httpx.WithEnvironmentScope(c.Request().Context(), nil)))
					}
					return next(c)
				}
			})
			group.Use(NewCompressionMiddleware())
			group.GET("/*", func(c *echo.Context) error {
				c.Response().Header().Set("Content-Length", strconv.Itoa(len(tt.body)))
				return c.Blob(http.StatusOK, tt.contentType, []byte(tt.body))
			})
			server := httptest.NewUnstartedServer(router)
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			req, err := http.NewRequest(http.MethodGet, server.URL+tt.path, http.NoBody)
			require.NoError(t, err)
			if tt.acceptGzip {
				req.Header.Set("Accept-Encoding", "gzip")
			}
			if tt.websocket {
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "websocket")
			}
			client := server.Client()
			client.Transport.(*http.Transport).DisableCompression = true
			defer client.CloseIdleConnections()
			response, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			raw, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode)
			if !tt.compressed {
				require.Empty(t, response.Header.Get("Content-Encoding"))
				require.Equal(t, tt.body, string(raw))
				return
			}
			require.Equal(t, 2, response.ProtoMajor)
			require.Equal(t, "gzip", response.Header.Get("Content-Encoding"))
			require.Equal(t, strconv.Itoa(len(raw)), response.Header.Get("Content-Length"))
			reader, err := gzip.NewReader(bytes.NewReader(raw))
			require.NoError(t, err)
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.Equal(t, tt.body, string(body))
		})
	}
}
