package edge

import (
	"context"
	"crypto/tls"
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
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		dialer.TLSClientConfig = tlsConfig
	}

	headers := http.Header{}
	for header, value := range agentAuthCredentialsInternal(c.cfg.AgentToken) {
		headers.Set(header, value)
	}

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

	return c.serveTunnelSessionInternal(ctx, NewTunnelConn(conn), managerWSURL)
}
