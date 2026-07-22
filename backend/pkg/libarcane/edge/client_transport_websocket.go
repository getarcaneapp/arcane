package edge

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/gorilla/websocket"
)

func (c *TunnelClient) connectAndServeWebSocket(ctx context.Context) error {
	managerWSURL := c.managerWebSocketURLInternal()
	if managerWSURL == "" {
		return errors.New("manager WebSocket URL is empty")
	}
	c.managerURL = managerWSURL

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 30 * time.Second,
	}
	if strings.HasPrefix(strings.ToLower(managerWSURL), "wss://") {
		tlsConfig, err := buildManagerClientTLSConfigInternal(c.cfg)
		if err != nil {
			return errors.WrapIf(err, "failed to configure edge websocket TLS")
		}
		dialer.TLSClientConfig = tlsConfig
	}

	headers := http.Header{}
	headers.Set(HeaderAgentToken, c.cfg.AgentToken)
	headers.Set(HeaderAPIKey, c.cfg.AgentToken)
	headers.Set(HeaderAuthorization, "Bearer "+c.cfg.AgentToken)

	slog.DebugContext(ctx, "Dialing manager for websocket edge tunnel", "url", managerWSURL)

	conn, resp, err := dialer.DialContext(ctx, managerWSURL, headers)
	if err != nil {
		if resp != nil {
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			return errors.WrapIff(err, "failed to connect to manager websocket endpoint (status: %d, body: %s)", resp.StatusCode, string(body))
		}
		return errors.WrapIf(err, "failed to connect to manager websocket endpoint")
	}
	defer func() { _ = conn.Close() }()

	tunnelConn := NewTunnelConn(conn)
	c.setConn(tunnelConn)
	setActiveAgentTunnelConn(tunnelConn)
	defer clearActiveAgentTunnelConn(tunnelConn)
	if err := tunnelConn.Send(c.registerMessageInternal()); err != nil {
		return errors.WrapIf(err, "failed to send websocket register message")
	}
	registerMsg, err := c.awaitRegistrationInternal(ctx)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "WebSocket edge tunnel connected to manager", "manager_url", managerWSURL)
	slog.InfoContext(ctx, "Edge websocket tunnel registered",
		"environment_id", registerMsg.EnvironmentID,
		"session_id", registerMsg.SessionID,
	)
	c.markTransportConnectedInternal(EdgeTransportWebSocket)

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go c.heartbeatLoop(connCtx)

	return c.messageLoop(connCtx)
}
