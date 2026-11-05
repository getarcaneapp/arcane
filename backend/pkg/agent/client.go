package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

const (
	// maxReconnectInterval caps the exponential reconnect backoff so a long-dead
	// manager is still retried at a sane cadence.
	maxReconnectInterval = 60 * time.Second
	// healthyTunnelSessionDuration is how long a tunnel session must survive
	// before the reconnect backoff is treated as recovered.
	healthyTunnelSessionDuration = 60 * time.Second

	// DefaultHeartbeatInterval is how often the client sends heartbeats
	DefaultHeartbeatInterval = 30 * time.Second
	// DefaultWriteTimeout is the timeout for write operations
	DefaultWriteTimeout = 10 * time.Second
	// DefaultGRPCRegistrationTimeout bounds how long the agent waits for the
	// manager to acknowledge tunnel registration on any transport.
	DefaultGRPCRegistrationTimeout = 10 * time.Second
	// DefaultWebSocketPreferenceTTL keeps websocket as the preferred transport
	// for a short period after a successful auto-mode fallback.
	DefaultWebSocketPreferenceTTL = 2 * time.Minute
	// maxWebSocketPreferenceTTL caps the backoff of the websocket preference window
	// while gRPC keeps failing, so a broken gRPC path is retried but never disabled.
	maxWebSocketPreferenceTTL = 30 * time.Minute
)

var (
	// errTunnelRegistrationTimeout marks a manager that accepted the connection but never answered registration.
	errTunnelRegistrationTimeout = errors.New("timed out waiting for tunnel registration response")
	// errEstablishedTunnelSessionEnded marks an accepted session the manager later dropped.
	errEstablishedTunnelSessionEnded = errors.New("established agent tunnel session ended")
)

func NewTunnelClient(cfg TunnelClientConfig) *TunnelClient {
	reconnectInterval := time.Duration(cfg.Config.AgentReconnectInterval) * time.Second
	if reconnectInterval < time.Second {
		reconnectInterval = 5 * time.Second
	}

	return &TunnelClient{
		cfg:                    cfg.Config,
		errCh:                  cfg.Errors,
		handler:                cfg.Handler,
		streamWindowMiB:        cfg.StreamWindowMiB,
		reconnectInterval:      reconnectInterval,
		registrationTimeout:    DefaultGRPCRegistrationTimeout,
		websocketPreferenceTTL: DefaultWebSocketPreferenceTTL,
		healthySessionDuration: healthyTunnelSessionDuration,
		managerGRPCAddr:        httpx.ManagerGRPCAddr(cfg.Config.MTLS.ManagerURL),
		agentInstanceID:        uuid.New().String(),
	}
}

func (c *TunnelClient) Start(ctx context.Context) (func(context.Context) error, error) {
	transport := NormalizeTransport(c.cfg.AgentTransport)
	switch {
	case !c.cfg.AgentEnabled:
		return nil, errors.New("agent tunnel disabled")
	case transport != TransportWebSocket && httpx.ManagerGRPCAddr(c.cfg.MTLS.ManagerURL) == "":
		return nil, errors.New("ARCANE_MANAGER_API_URL with a valid host is required for gRPC transport")
	case transport != TransportGRPC && strings.TrimSpace(httpx.ManagerBaseURL(c.cfg.MTLS.ManagerURL)) == "":
		return nil, errors.New("ARCANE_MANAGER_API_URL is required for websocket transport")
	case c.cfg.AgentToken == "":
		return nil, errors.New("ARCANE_AGENT_TOKEN is required")
	}
	if parsed, err := url.Parse(httpx.ManagerBaseURL(c.cfg.MTLS.ManagerURL)); transport == TransportGRPC && err == nil && strings.Trim(parsed.Path, "/") != "" {
		return nil, errors.New("gRPC transport cannot use an ARCANE_MANAGER_API_URL with a path prefix; use ARCANE_AGENT_TRANSPORT=websocket or auto")
	}
	if c.cfg.MTLS.Mode != MTLSModeDisabled && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(httpx.ManagerBaseURL(c.cfg.MTLS.ManagerURL))), "https://") {
		return nil, errors.New("ARCANE_AGENT_MTLS_MODE requires ARCANE_MANAGER_API_URL to use https")
	}

	return concurrency.Supervise(ctx, concurrency.SuperviseConfig{
		Run: func(runCtx context.Context) error {
			c.run(runCtx)
			return nil
		},
		Name: "Agent tunnel client",
	}).Stop, nil
}

func (c *TunnelClient) run(ctx context.Context) {
	if c.errCh != nil {
		defer close(c.errCh)
	}
	if parsed, err := url.Parse(httpx.ManagerBaseURL(c.cfg.MTLS.ManagerURL)); err == nil && strings.Trim(parsed.Path, "/") != "" && c.managerGRPCAddr != "" {
		slog.WarnContext(ctx, "ARCANE_MANAGER_API_URL has a path prefix that gRPC cannot route; gRPC agent tunnel transport is unavailable", "pathPrefix", parsed.Path)
		c.managerGRPCAddr = ""
	}
	// Log only the endpoints this agent will dial.
	attrs := []any{"securityMode", c.cfg.MTLS.Mode}
	transports := c.managedTunnelTransports()
	if transports.grpc {
		attrs = append(attrs, "grpcAddr", c.managerGRPCAddr)
	}
	if transports.websocket {
		attrs = append(attrs, "websocketUrl", c.managerWebSocketURL())
	}
	slog.InfoContext(ctx, "Starting agent session client", attrs...)
	// Back off exponentially (with jitter) so a broken or unauthorized agent does not hammer the
	// manager, resetting only once a session has stayed up long enough to count as healthy.
	reconnectBackoff := backoff.NewExponentialBackOff()
	reconnectBackoff.InitialInterval = c.reconnectInterval
	reconnectBackoff.MaxInterval = maxReconnectInterval
	serve := func(ctx context.Context) error {
		httpClient, err := c.cfg.MTLS.ManagerClient(ctx, ManagerClientOptions{
			AgentToken: c.cfg.AgentToken,
			Enroll:     true,
		})
		if err != nil {
			return err
		}
		return c.connectAndServeManagedTunnel(ctx, httpClient)
	}

	for {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "Agent tunnel client shutting down")
			return
		}
		sessionStart := time.Now()
		if err := serve(ctx); err != nil {
			slog.WarnContext(ctx, "Agent tunnel disconnected", "error", err)
			select {
			case c.errCh <- err:
			default:
			}
		}
		var delay time.Duration
		if time.Since(sessionStart) >= c.healthySessionDuration {
			// Redial a dropped healthy session almost immediately, jittered to avoid a stampede.
			reconnectBackoff.Reset()
			delay = 100*time.Millisecond + rand.N(150*time.Millisecond)
		} else {
			delay = reconnectBackoff.NextBackOff()
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			slog.InfoContext(ctx, "Attempting to reconnect agent tunnel", "delay", delay)
		}
	}
}

func (c *TunnelClient) connectAndServeManagedTunnel(ctx context.Context, httpClient *http.Client) error {
	transports := c.managedTunnelTransports()
	preference := kit.FromPtr(c.preference.Load())
	switch {
	case !transports.grpc && !transports.websocket:
		return errors.New("no agent tunnel transport is available")
	case !transports.grpc:
		return c.connectAndServeWebSocket(ctx, httpClient)
	case transports.websocket && time.Now().Before(preference.webSocketUntil):
		slog.InfoContext(ctx, "Temporarily preferring websocket agent tunnel transport after recent websocket success",
			"preferredUntil", preference.webSocketUntil,
			"grpcFailureStreak", preference.grpcFailures,
			"managerWsUrl", c.managerWebSocketURL(),
		)
		return c.connectAndServeWebSocket(ctx, httpClient)
	}

	sessionStart := time.Now()
	err := c.connectAndServeGRPC(ctx)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errEstablishedTunnelSessionEnded) && time.Since(sessionStart) >= c.healthySessionDuration:
		return err
	}

	failed := kit.FromPtr(c.preference.Load())
	c.preference.Store(&transportPreference{
		webSocketUntil: failed.webSocketUntil,
		grpcFailures:   failed.grpcFailures + 1,
	})
	if !transports.websocket {
		return err
	}
	slog.WarnContext(ctx, "gRPC agent tunnel connection failed, falling back to websocket transport",
		"error", err,
		"managerGrpcAddr", c.managerGRPCAddr,
		"managerWsUrl", c.managerWebSocketURL(),
	)
	if wsErr := c.connectAndServeWebSocket(ctx, httpClient); wsErr != nil {
		return fmt.Errorf("gRPC agent tunnel failed: %w; websocket fallback failed: %w", err, wsErr)
	}
	return nil
}

func (c *TunnelClient) managedTunnelTransports() tunnelTransports {
	managerGRPCAvailable := strings.TrimSpace(c.managerGRPCAddr) != ""
	managerWebSocketAvailable := c.managerWebSocketURL() != ""
	switch NormalizeTransport(c.cfg.AgentTransport) {
	case TransportAuto:
		return tunnelTransports{grpc: managerGRPCAvailable, websocket: managerWebSocketAvailable}
	case TransportWebSocket:
		return tunnelTransports{websocket: managerWebSocketAvailable}
	case TransportGRPC:
		return tunnelTransports{grpc: managerGRPCAvailable}
	default:
		return tunnelTransports{}
	}
}

func (c *TunnelClient) managerWebSocketURL() string {
	managerBaseURL := strings.TrimRight(strings.TrimSpace(httpx.ManagerBaseURL(c.cfg.MTLS.ManagerURL)), "/")
	if managerBaseURL == "" {
		return strings.TrimSpace(c.managerURL)
	}
	return cmp.Or(strings.TrimSpace(c.managerURL), HTTPToWebSocketURL(managerBaseURL)+tunnelConnectPath)
}

func (c *TunnelClient) awaitRegistration(ctx context.Context, conn TunnelConnection) (*TunnelMessage, error) {
	type registrationResult struct {
		msg *TunnelMessage
		err error
	}

	recvCh := make(chan registrationResult, 1)
	go func() {
		msg, err := conn.Receive()
		recvCh <- registrationResult{msg: msg, err: err}
	}()

	timer := time.NewTimer(c.registrationTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	case <-timer.C:
		_ = conn.Close()
		return nil, fmt.Errorf("after %s: %w", c.registrationTimeout, errTunnelRegistrationTimeout)
	case result := <-recvCh:
		if result.err != nil {
			return nil, fmt.Errorf("failed to receive tunnel registration response: %w", result.err)
		}
		if result.msg == nil {
			return nil, errors.New("received empty tunnel registration response")
		}
		if result.msg.Type != MessageTypeRegisterResponse {
			return nil, fmt.Errorf("unexpected first tunnel message: %s", result.msg.Type)
		}
		if !result.msg.Accepted {
			return nil, fmt.Errorf("manager rejected tunnel registration: %s", result.msg.Error)
		}
		c.sessionID.Store(result.msg.SessionID)
		return result.msg, nil
	}
}

// serveTunnelSession registers on an established tunnel connection and serves its multiplexed session,
// for every transport.
func (c *TunnelClient) serveTunnelSession(ctx context.Context, conn TunnelConnection, managerAddr string) error {
	box := &connBox{conn: conn}
	c.conn.Store(box)
	connCtx, connCancel := context.WithCancel(ctx)
	stopCloseWatch := context.AfterFunc(connCtx, func() { _ = conn.Close() })
	defer func() {
		connCancel()
		_ = conn.Close()
		stopCloseWatch()
		c.conn.CompareAndSwap(box, nil)
	}()

	sessionID, _ := c.sessionID.Load()
	streamWindowMiB := c.streamWindowMiB()
	if err := conn.Send(&TunnelMessage{
		Type:          MessageTypeRegister,
		AgentToken:    c.cfg.AgentToken,
		AgentInstance: c.agentInstanceID,
		Capabilities:  []string{tunnelCapabilityMux, TunnelCapabilityForwardedPermissions, tunnelCapabilityMuxWindowPrefix + strconv.Itoa(streamWindowMiB)},
		ResumeSession: sessionID,
		Host:          &c.cfg.HostInfo,
	}); err != nil {
		// A rejected gRPC stream can report EOF from Send; Recv carries the RPC status.
		if conn.Transport() != TransportGRPC || !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to send %s tunnel register message: %w", conn.Transport(), err)
		}
	}

	registerResp, err := c.awaitRegistration(ctx, conn)
	if err != nil {
		return err
	}
	if !slices.Contains(registerResp.Capabilities, tunnelCapabilityMuxGrant) {
		return errManagerMuxRequired
	}

	slog.InfoContext(ctx, "Agent tunnel connected to manager",
		"transport", conn.Transport(),
		"managerAddr", managerAddr,
		"environmentId", registerResp.EnvironmentID,
		"sessionId", registerResp.SessionID,
	)
	transports := c.managedTunnelTransports()
	switch {
	case conn.Transport() == TransportGRPC:
		c.preference.Store(&transportPreference{})
	case conn.Transport() == TransportWebSocket && transports.grpc && transports.websocket:
		grpcFailures := kit.FromPtr(c.preference.Load()).grpcFailures
		// Back off the preference window while gRPC keeps failing so reconnects do not re-pay its timeout.
		c.preference.Store(&transportPreference{
			webSocketUntil: time.Now().Add(min(c.websocketPreferenceTTL<<min(grpcFailures, 4), maxWebSocketPreferenceTTL)),
			grpcFailures:   grpcFailures,
		})
	}

	return fmt.Errorf("%w: %w", errEstablishedTunnelSessionEnded, c.serveMuxSession(connCtx, conn.muxConn(connCtx), streamWindowMiB))
}

// agentAuthCredentials returns the canonical header/metadata credential
// set every agent transport presents, so the three transports cannot drift.
func agentAuthCredentials(token string) map[string]string {
	return map[string]string{
		HeaderAgentToken:    token,
		HeaderAPIKey:        token,
		HeaderAuthorization: bearerScheme + token,
	}
}

func flattenResponseHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func (c *TunnelClient) connectAndServeWebSocket(ctx context.Context, httpClient *http.Client) error {
	managerWSURL := c.managerWebSocketURL()
	if managerWSURL == "" {
		return errors.New("manager WebSocket URL is empty")
	}

	headers := http.Header{}
	for header, value := range agentAuthCredentials(c.cfg.AgentToken) {
		headers.Set(header, value)
	}

	slog.DebugContext(ctx, "Dialing manager for websocket agent tunnel", "url", managerWSURL)

	// The dial context bounds only the handshake; the connection outlives it.
	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()
	conn, resp, err := websocket.Dial(dialCtx, managerWSURL, &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return fmt.Errorf("failed to connect to manager websocket endpoint (status: %d, body: %s): %w", resp.StatusCode, strings.TrimSpace(string(body)), err)
		}
		return fmt.Errorf("failed to connect to manager websocket endpoint: %w", err)
	}

	return c.serveTunnelSession(ctx, NewWebSocketTunnelConn(conn), managerWSURL)
}
