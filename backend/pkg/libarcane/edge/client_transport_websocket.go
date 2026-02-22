package edge

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	"github.com/gorilla/websocket"
)

func (c *TunnelClient) connectAndServeWebSocket(ctx context.Context) error {
	managerWSURL := c.managerWebSocketURLInternal()
	if managerWSURL == "" {
		return fmt.Errorf("manager WebSocket URL is empty")
	}
	c.managerURL = managerWSURL

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 30 * time.Second,
	}

	headers := http.Header{}
	headers.Set(remenv.HeaderAgentToken, c.cfg.AgentToken)
	headers.Set(remenv.HeaderAPIKey, c.cfg.AgentToken)

	slog.DebugContext(ctx, "Dialing manager for websocket edge tunnel", "url", managerWSURL)

	conn, resp, err := dialer.DialContext(ctx, managerWSURL, headers)
	if err != nil {
		if resp != nil {
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to connect to manager websocket endpoint: %w, status: %d, body: %s", err, resp.StatusCode, string(body))
		}
		return fmt.Errorf("failed to connect to manager websocket endpoint: %w", err)
	}
	defer func() { _ = conn.Close() }()

	c.conn = NewTunnelConn(conn)
	setActiveAgentTunnelConn(c.conn)
	defer clearActiveAgentTunnelConn(c.conn)
	slog.InfoContext(ctx, "WebSocket edge tunnel connected to manager", "manager_url", managerWSURL)

	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go c.heartbeatLoop(heartbeatCtx)

	return c.messageLoop(ctx)
}
