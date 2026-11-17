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
)

func TestCompressionMiddlewareMatchesContentLengthOverHTTP2(t *testing.T) {
	large := `{"message":"` + strings.Repeat("arcane", 512) + `"}`
	tests := []struct {
		name       string
		body       string
		header     http.Header
		compressed bool
	}{
		{
			name:       "large JSON",
			body:       large,
			compressed: true,
		},
		{
			name: "small JSON",
			body: `{}`,
		},
		{
			name:   "event stream request",
			body:   large,
			header: http.Header{"Accept": {"text/event-stream"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := echo.New()
			group := router.Group("/api")
			group.Use(NewCompressionMiddleware())
			group.GET("/data", func(c *echo.Context) error {
				c.Response().Header().Set("Content-Length", strconv.Itoa(len(tt.body)))
				return c.Blob(http.StatusOK, "application/json", []byte(tt.body))
			})
			server := httptest.NewUnstartedServer(router)
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()

			req, err := http.NewRequest(http.MethodGet, server.URL+"/api/data", http.NoBody)
			require.NoError(t, err)
			for key, values := range tt.header {
				req.Header[key] = values
			}
			req.Header.Set("Accept-Encoding", "gzip")

			client := server.Client()
			client.Transport.(*http.Transport).DisableCompression = true
			defer client.CloseIdleConnections()
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() {
				_ = resp.Body.Close()
			}()
			raw, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t, 2, resp.ProtoMajor)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, strconv.Itoa(len(raw)), resp.Header.Get("Content-Length"))
			if !tt.compressed {
				require.Empty(t, resp.Header.Get("Content-Encoding"))
				require.Equal(t, tt.body, string(raw))
				return
			}
			require.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
			reader, err := gzip.NewReader(bytes.NewReader(raw))
			require.NoError(t, err)
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, tt.body, string(body))
		})
	}
}
