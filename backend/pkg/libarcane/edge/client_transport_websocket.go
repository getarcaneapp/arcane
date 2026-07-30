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

	"github.com/coder/websocket"
)

func (c *TunnelClient) connectAndServeWebSocket(ctx context.Context) error {
	managerWSURL := c.managerWebSocketURLInternal()
	if managerWSURL == "" {
		return errors.New("manager WebSocket URL is empty")
	}
	c.managerURL = managerWSURL

	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if strings.HasPrefix(strings.ToLower(managerWSURL), "wss://") {
		tlsConfig, err := buildManagerClientTLSConfigInternal(c.cfg)
		if err != nil {
			return errors.WrapIf(err, "failed to configure edge websocket TLS")
		}
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		transport.TLSClientConfig = tlsConfig
	}

	headers := http.Header{}
	for header, value := range agentAuthCredentialsInternal(c.cfg.AgentToken) {
		headers.Set(header, value)
	}

	slog.DebugContext(ctx, "Dialing manager for websocket edge tunnel", "url", managerWSURL)

	// The dial context bounds only the handshake; the connection outlives it.
	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()
	conn, resp, err := websocket.Dial(dialCtx, managerWSURL, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
		HTTPHeader: headers,
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			return errors.WrapIff(err, "failed to connect to manager websocket endpoint (status: %d, body: %s)", resp.StatusCode, string(body))
		}
		return errors.WrapIf(err, "failed to connect to manager websocket endpoint")
	}

	return c.serveTunnelSessionInternal(ctx, NewTunnelConn(conn), managerWSURL)
}
