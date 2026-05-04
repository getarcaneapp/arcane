package edge

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelClient_InternalRequestSkipsSlogGin(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
			loggerMiddleware := sloggin.NewWithConfig(slog.Default(), sloggin.Config{})

			router := gin.New()
			router.Use(func(c *gin.Context) {
				if IsInternalTunnelRequest(c.Request.Context()) {
					sawInternalTunnelRequest = true
					c.Next()
					return
				}
				loggerMiddleware(c)
			})
			router.GET("/local/api", func(c *gin.Context) {
				c.String(http.StatusOK, "local response")
			})

			client := NewTunnelClient(&Config{}, router)
			testCase.run(t, client)

			assert.True(t, sawInternalTunnelRequest)
		})
	}
}
