package edge

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	slogecho "github.com/samber/slog-echo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelClient_InternalRequestSkipsSlogEcho(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, client *TunnelClient)
	}{
		{
			name: "legacy response",
			run: func(t *testing.T, client *TunnelClient) {
				conn := &capturingTunnelConnForHandleRequest{}
				client.conn = conn

				client.handleRequest(context.Background(), &TunnelMessage{
					ID:     "req-legacy",
					Type:   MessageTypeRequest,
					Method: http.MethodGet,
					Path:   "/local/api",
				})

				require.Len(t, conn.sent, 1)
				assert.Equal(t, MessageTypeResponse, conn.sent[0].Type)
				assert.Equal(t, http.StatusOK, conn.sent[0].Status)
				assert.Equal(t, "local response", string(conn.sent[0].Body))
			},
		},
		{
			name: "streaming response",
			run: func(t *testing.T, client *TunnelClient) {
				conn := &fakeTunnelConn{}
				client.conn = conn

				client.handleRequestStreaming(context.Background(), &TunnelMessage{
					ID:     "req-stream",
					Type:   MessageTypeRequest,
					Method: http.MethodGet,
					Path:   "/local/api",
				})

				require.Len(t, conn.msgs, 3)
				assert.Equal(t, MessageTypeResponse, conn.msgs[0].Type)
				assert.Equal(t, http.StatusOK, conn.msgs[0].Status)
				assert.Equal(t, MessageTypeStreamData, conn.msgs[1].Type)
				assert.Equal(t, "local response", string(conn.msgs[1].Body))
				assert.Equal(t, MessageTypeStreamEnd, conn.msgs[2].Type)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var sawInternalTunnelRequest bool
			loggerMiddleware := slogecho.New(slog.Default())

			router := echo.New()
			router.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					if IsInternalTunnelRequest(c.Request().Context()) {
						sawInternalTunnelRequest = true
						return next(c)
					}
					return loggerMiddleware(next)(c)
				}
			})
			router.GET("/local/api", func(c echo.Context) error {
				return c.String(http.StatusOK, "local response")
			})

			client := NewTunnelClient(&Config{}, router)
			testCase.run(t, client)

			assert.True(t, sawInternalTunnelRequest)
		})
	}
}
