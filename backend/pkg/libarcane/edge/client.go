package edge

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	kit "go.getarcane.app/kit/pkg"

	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

const (
	// maxReconnectInterval caps the exponential reconnect backoff so a long-dead
	// manager is still retried at a sane cadence.
	maxReconnectInterval = 60 * time.Second
	// healthyTunnelSessionDuration is how long a tunnel session must survive
	// before the reconnect backoff is treated as recovered.
	healthyTunnelSessionDuration = 60 * time.Second
	// maxPollRetryInterval caps the exponential backoff applied to failed poll
	// control requests. Successful polls return to the manager-advertised interval.
	maxPollRetryInterval            = 60 * time.Second
	tunnelSessionWorkerDrainTimeout = 10 * time.Second

	// DefaultHeartbeatInterval is how often the client sends heartbeats
	DefaultHeartbeatInterval = 30 * time.Second
	// DefaultWriteTimeout is the timeout for write operations
	DefaultWriteTimeout = 10 * time.Second
	// DefaultRequestTimeout is the timeout for executing local requests
	DefaultRequestTimeout = 5 * time.Minute
	// DefaultGRPCRegistrationTimeout bounds how long the agent waits for the
	// manager to acknowledge tunnel registration on any transport.
	DefaultGRPCRegistrationTimeout = 10 * time.Second
	// DefaultWebSocketPreferenceTTL keeps websocket as the preferred transport
	// for a short period after a successful auto-mode fallback.
	DefaultWebSocketPreferenceTTL = 2 * time.Minute
	// maxWebSocketPreferenceTTL caps the backoff of the websocket preference window
	// while gRPC keeps failing, so a broken gRPC path is retried but never disabled.
	maxWebSocketPreferenceTTL = 30 * time.Minute
	defaultCommandChunkSize   = 256 * 1024
)

var (
	// errTunnelRegistrationTimeout marks a manager that accepted the connection but never answered registration.
	errTunnelRegistrationTimeout = errors.New("timed out waiting for tunnel registration response")
	// errEstablishedTunnelSessionEnded marks an accepted session the manager later dropped.
	errEstablishedTunnelSessionEnded = errors.New("established edge tunnel session ended")
)

// localDialSkipHeaders lists headers not forwarded when the agent dials its own server for a
// proxied WebSocket: handshake headers and browser headers that fail the local origin check.
var localDialSkipHeaders = map[string]bool{
	"Sec-Websocket-Key":        true,
	"Sec-Websocket-Version":    true,
	"Sec-Websocket-Extensions": true,
	"Upgrade":                  true,
	"Connection":               true,
	"Host":                     true,
	"Origin":                   true,
	"Cookie":                   true,
	"Authorization":            true,
	"Referer":                  true,
	"Sec-Fetch-Dest":           true,
	"Sec-Fetch-Mode":           true,
	"Sec-Fetch-Site":           true,
	"Sec-Fetch-User":           true,
	"Sec-Ch-Ua":                true,
	"Sec-Ch-Ua-Mobile":         true,
	"Sec-Ch-Ua-Platform":       true,
}

func (t *commandRequestTransfer) stop() {
	t.timerMu.Lock()
	defer t.timerMu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
	}
}

// NewTunnelClient creates a new tunnel client
func NewTunnelClient(cfg *Config, handler http.Handler) *TunnelClient {
	reconnectInterval := time.Duration(cfg.EdgeReconnectInterval) * time.Second
	if reconnectInterval < time.Second {
		reconnectInterval = 5 * time.Second
	}

	return &TunnelClient{
		cfg:                    cfg,
		handler:                handler,
		reconnectInterval:      reconnectInterval,
		heartbeatInterval:      DefaultHeartbeatInterval,
		registrationTimeout:    DefaultGRPCRegistrationTimeout,
		websocketPreferenceTTL: DefaultWebSocketPreferenceTTL,
		healthySessionDuration: healthyTunnelSessionDuration,
		managerGRPCAddr:        httpx.ManagerGRPCAddr(cfg.ManagerApiUrl),
		localPort:              cmp.Or(cfg.Port, "3552"),
		requestTimeout:         DefaultRequestTimeout,
		agentInstanceID:        uuid.New().String(),
	}
}

// StartWithErrorChan runs the tunnel client and optionally emits connection errors.
func (c *TunnelClient) StartWithErrorChan(ctx context.Context, errCh chan error) {
	if errCh != nil {
		defer close(errCh)
	}
	slog.InfoContext(ctx, "Starting edge agent session client", StartupLogAttrs(c.cfg)...)
	// Back off exponentially (with jitter) so a broken or unauthorized agent does not hammer the
	// manager, resetting only once a session has stayed up long enough to count as healthy.
	reconnectBackoff := backoff.NewExponentialBackOff()
	reconnectBackoff.InitialInterval = c.reconnectInterval
	reconnectBackoff.MaxInterval = maxReconnectInterval
	serve := kit.Ternary(UsePollEdgeTransport(c.cfg), c.connectAndServePoll, c.connectAndServeManagedTunnel)

	for {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "Edge tunnel client shutting down")
			return
		}
		sessionStart := time.Now()
		if err := serve(ctx); err != nil {
			slog.WarnContext(ctx, "Edge tunnel disconnected", "error", err)
			if errCh != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}
		if time.Since(sessionStart) >= c.healthySessionDuration {
			reconnectBackoff.Reset()
		}

		delay := reconnectBackoff.NextBackOff()
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			slog.InfoContext(ctx, "Attempting to reconnect edge tunnel", "delay", delay)
		}
	}
}

// StartupLogAttrs returns the slog attributes describing the agent's edge transport configuration.
func StartupLogAttrs(cfg *Config) []any {
	if cfg == nil {
		return []any{
			"control_plane", "unknown",
			"managed_session_transports",
			[]string{},
			"security_mode", NormalizeEdgeMTLSMode(""),
		}
	}

	poll := UsePollEdgeTransport(cfg)
	managerGRPCAddr := strings.TrimSpace(httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	managerBaseURL := strings.TrimSpace(httpx.ManagerBaseURL(cfg.ManagerApiUrl))
	managedSessionTransports := make([]string, 0, 2)
	if UseGRPCEdgeTransport(cfg) || (poll && managerGRPCAddr != "") {
		managedSessionTransports = append(managedSessionTransports, EdgeTransportGRPC)
	}
	if UseWebSocketEdgeTransport(cfg) || (poll && managerBaseURL != "") {
		managedSessionTransports = append(managedSessionTransports, EdgeTransportWebSocket)
	}

	attrs := []any{
		"control_plane", kit.Ternary(poll, EdgeTransportPoll, "managed"),
		"managed_session_transports", managedSessionTransports,
		"security_mode", NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode),
	}
	if managerAPIURL := strings.TrimSpace(cfg.ManagerApiUrl); managerAPIURL != "" {
		attrs = append(attrs, "manager_api_url", managerAPIURL)
	}
	if managerGRPCAddr != "" {
		attrs = append(attrs, "manager_grpc_addr", managerGRPCAddr)
	}
	if managerBaseURL != "" {
		attrs = append(attrs, "manager_base_url", managerBaseURL)
	}
	return attrs
}

func (c *TunnelClient) connectAndServeManagedTunnel(ctx context.Context) error {
	transports := c.managedTunnelTransports()
	if transports.grpc {
		c.transportPreferenceMu.RLock()
		preferredUntil, grpcFailureStreak := c.preferWebSocketUntil, c.grpcFailureStreak
		c.transportPreferenceMu.RUnlock()
		if transports.websocket && time.Now().Before(preferredUntil) {
			slog.InfoContext(ctx, "Temporarily preferring websocket edge tunnel transport after recent websocket success",
				"preferredUntil", preferredUntil,
				"grpcFailureStreak", grpcFailureStreak,
				"managerWsUrl", c.managerWebSocketURL(),
			)
			return c.connectAndServeWebSocket(ctx)
		}

		sessionStart := time.Now()
		if err := c.connectAndServeGRPC(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A healthy session the manager dropped (restart, redeploy) is not a broken gRPC path:
			// let the outer loop back off and retry gRPC instead of racing a websocket dial.
			if errors.Is(err, errEstablishedTunnelSessionEnded) && time.Since(sessionStart) >= c.healthySessionDuration {
				return err
			}
			c.transportPreferenceMu.Lock()
			c.grpcFailureStreak++
			c.transportPreferenceMu.Unlock()
			if transports.websocket {
				slog.WarnContext(ctx, "gRPC edge tunnel connection failed, falling back to websocket transport",
					"error", err,
					"managerGrpcAddr", c.managerGRPCAddr,
					"managerWsUrl", c.managerWebSocketURL(),
				)
				if wsErr := c.connectAndServeWebSocket(ctx); wsErr != nil {
					// Keep both transport failures in the chain for errors.Is/errors.As traversal.
					return fmt.Errorf("gRPC edge tunnel failed: %w; websocket fallback failed: %w", err, wsErr)
				}
				return nil
			}
			return err
		}
		return nil
	}
	if transports.websocket {
		return c.connectAndServeWebSocket(ctx)
	}
	return errors.New("no edge tunnel transport is available")
}

func (c *TunnelClient) managedTunnelTransports() managedTunnelTransportsInternal {
	if c == nil || c.cfg == nil {
		return managedTunnelTransportsInternal{}
	}

	managerGRPCAvailable := strings.TrimSpace(c.managerGRPCAddr) != ""
	managerWebSocketAvailable := c.managerWebSocketURL() != ""
	switch NormalizeEdgeTransport(c.cfg.EdgeTransport) {
	case EdgeTransportAuto, EdgeTransportPoll:
		return managedTunnelTransportsInternal{grpc: managerGRPCAvailable, websocket: managerWebSocketAvailable}
	case EdgeTransportWebSocket:
		return managedTunnelTransportsInternal{websocket: managerWebSocketAvailable}
	case EdgeTransportGRPC:
		return managedTunnelTransportsInternal{grpc: managerGRPCAvailable}
	default:
		return managedTunnelTransportsInternal{}
	}
}

func (c *TunnelClient) managerWebSocketURL() string {
	if managerURL := strings.TrimSpace(c.managerURL); managerURL != "" {
		return managerURL
	}
	if c.cfg == nil {
		return ""
	}
	managerBaseURL := strings.TrimRight(strings.TrimSpace(httpx.ManagerBaseURL(c.cfg.ManagerApiUrl)), "/")
	if managerBaseURL == "" {
		return ""
	}
	return HTTPToWebSocketURL(managerBaseURL) + "/api/tunnel/connect"
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
		c.registration.Store(clientRegistrationInternal{
			sessionID:     result.msg.SessionID,
			commandCredit: slices.Contains(result.msg.Capabilities, tunnelCapabilityCommandCreditGrant),
		})
		return result.msg, nil
	}
}

// serveTunnelSession runs the shared register/heartbeat/message lifecycle on an established
// tunnel connection, for every transport.
func (c *TunnelClient) serveTunnelSession(ctx context.Context, conn TunnelConnection, managerAddr string) error {
	box := &connBox{conn: conn}
	c.conn.Store(box)
	setActiveAgentTunnelConn(conn)
	connCtx, connCancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	stopCloseWatch := context.AfterFunc(connCtx, func() { _ = conn.Close() })
	defer func() {
		connCancel()
		// Tear down streams and pending transfers so a reconnect cannot leak goroutines or sockets.
		c.activeStreams.Range(func(key, value any) bool {
			streamID, idOK := key.(string)
			stream, streamOK := value.(*activeWSStream)
			if idOK && streamOK {
				c.closeWebSocketStream(streamID, stream)
			}
			return true
		})
		c.requestTransfers.Range(func(transferID, value any) bool {
			switch transfer, ok := value.(*commandRequestTransfer); {
			case !ok:
				c.requestTransfers.Delete(transferID)
			case transfer.conn == conn && c.requestTransfers.CompareAndDelete(transferID, transfer):
				transfer.stop()
			}
			return true
		})
		_ = conn.Close()
		stopCloseWatch()
		drainCtx, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), tunnelSessionWorkerDrainTimeout)
		if err := utils.WaitGroup(drainCtx, &workers); err != nil {
			slog.WarnContext(context.WithoutCancel(ctx), "Timed out waiting for edge tunnel session workers", "error", err)
		}
		cancelDrain()
		c.conn.CompareAndSwap(box, nil)
		clearActiveAgentTunnelConn(conn)
	}()

	registration, _ := c.registration.Load()
	registerMsg := &TunnelMessage{
		Type:          MessageTypeRegister,
		AgentToken:    c.cfg.AgentToken,
		AgentInstance: c.agentInstanceID,
		Capabilities:  append(AdvertisedEdgeCommands(), tunnelCapabilityChunkedRequest, tunnelCapabilityProtoParity, tunnelCapabilityCommandCredit),
		ResumeSession: registration.sessionID,
	}
	if err := conn.Send(registerMsg); err != nil {
		// A rejected gRPC stream can report EOF from Send; Recv carries the RPC status.
		if conn.Transport() != EdgeTransportGRPC || !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to send %s tunnel register message: %w", conn.Transport(), err)
		}
	}

	registerResp, err := c.awaitRegistration(ctx, conn)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Edge tunnel connected to manager",
		"transport", conn.Transport(),
		"managerAddr", managerAddr,
		"environmentId", registerResp.EnvironmentID,
		"sessionId", registerResp.SessionID,
	)
	c.transportPreferenceMu.Lock()
	switch conn.Transport() {
	case EdgeTransportGRPC:
		c.preferWebSocketUntil, c.grpcFailureStreak = time.Time{}, 0
	case EdgeTransportWebSocket:
		if transports := c.managedTunnelTransports(); transports.grpc && transports.websocket {
			// Back off the preference window while gRPC keeps failing so reconnects do not re-pay its timeout.
			ttl := min(c.websocketPreferenceTTL<<min(c.grpcFailureStreak, 4), maxWebSocketPreferenceTTL)
			c.preferWebSocketUntil = time.Now().Add(ttl)
		}
	}
	c.transportPreferenceMu.Unlock()

	workers.Go(func() { c.heartbeatLoop(connCtx, conn) })

	return fmt.Errorf("%w: %w", errEstablishedTunnelSessionEnded, c.messageLoop(connCtx, conn, &workers))
}

// heartbeatLoop sends periodic heartbeats
func (c *TunnelClient) heartbeatLoop(ctx context.Context, conn TunnelConnection) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if conn == nil || conn.IsClosed() {
				return
			}

			if err := conn.Send(&TunnelMessage{ID: uuid.New().String(), Type: MessageTypeHeartbeat}); err != nil {
				slog.WarnContext(ctx, "Failed to send heartbeat", "error", err)
				// Force reconnect so the manager does not keep stale state without heartbeats.
				if closeErr := conn.Close(); closeErr != nil {
					slog.DebugContext(ctx, "Failed to close tunnel connection after heartbeat failure", "error", closeErr)
				}
				return
			}
			slog.DebugContext(ctx, "Sent heartbeat to manager")
		}
	}
}

// messageLoop processes incoming messages from the manager
func (c *TunnelClient) messageLoop(ctx context.Context, conn TunnelConnection, workers *sync.WaitGroup) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := conn.Receive()
		if err != nil {
			return fmt.Errorf("failed to receive message: %w", err)
		}

		switch msg.Type {
		case MessageTypeRequest:
			workers.Go(func() { c.handleRequest(ctx, conn, msg) })
		case MessageTypeCommandRequest:
			if transferID := strings.TrimSpace(msg.Metadata[bodyTransferMetadataKey]); transferID != "" {
				c.beginCommandRequestTransfer(ctx, conn, transferID, msg)
			} else {
				workers.Go(func() { c.handleCommandRequest(ctx, conn, msg) })
			}
		case MessageTypeFileChunk:
			c.handleCommandRequestChunk(ctx, msg, workers)
		case MessageTypeWebSocketStart, MessageTypeStreamOpen:
			c.handleWebSocketStart(ctx, conn, msg, workers)
		case MessageTypeWebSocketData, MessageTypeStreamData:
			c.handleStreamData(ctx, msg)
		case MessageTypeWebSocketClose, MessageTypeStreamClose:
			if value, ok := c.activeStreams.Load(msg.ID); ok {
				if stream, isStream := value.(*activeWSStream); isStream {
					c.closeWebSocketStream(msg.ID, stream)
					slog.DebugContext(ctx, "Closed WebSocket stream", "streamId", msg.ID)
				}
			}
		case MessageTypeCancelRequest:
			slog.DebugContext(ctx, "Ignoring edge cancel request on agent", "id", msg.ID)
		case MessageTypeCommandCredit:
			if value, ok := c.commandRecorders.Load(msg.ID); ok {
				if recorder, isRecorder := value.(*commandResponseRecorder); isRecorder {
					recorder.inFlight.Add(-msg.Credit)
					select {
					case recorder.credited <- struct{}{}:
					default:
					}
				}
			}
		case MessageTypeResponse, MessageTypeHeartbeat, MessageTypeStreamEnd, MessageTypeEvent, MessageTypeCommandAck, MessageTypeCommandOutput, MessageTypeCommandComplete:
			slog.DebugContext(ctx, "Ignoring message type on agent", "type", msg.Type)
		case MessageTypeHeartbeatAck:
			slog.DebugContext(ctx, "Received heartbeat ack")
		case MessageTypeRegisterResponse:
			if !msg.Accepted {
				return fmt.Errorf("manager rejected tunnel registration: %s", msg.Error)
			}
			slog.InfoContext(ctx, "Edge tunnel re-registered",
				"transport", conn.Transport(),
				"environmentId", msg.EnvironmentID,
			)
		case MessageTypeRegister:
			slog.DebugContext(ctx, "Ignoring register message on agent")
		default:
			slog.WarnContext(ctx, "Unknown message type", "type", msg.Type)
		}
	}
}

func (c *TunnelClient) beginCommandRequestTransfer(ctx context.Context, conn TunnelConnection, transferID string, msg *TunnelMessage) {
	if c == nil || conn == nil || msg == nil {
		return
	}

	transfer := &commandRequestTransfer{request: msg, conn: conn}
	transfer.timerMu.Lock()
	previous, loaded := c.requestTransfers.Swap(transferID, transfer)
	transfer.timer = time.AfterFunc(c.requestTimeout, func() {
		if c.requestTransfers.CompareAndDelete(transferID, transfer) {
			slog.WarnContext(ctx, "Command body transfer expired", "transferId", transferID, "commandId", msg.ID)
			c.sendCommandComplete(conn, msg.ID, http.StatusRequestTimeout, "command body transfer timed out")
		}
	})
	transfer.timerMu.Unlock()
	if loaded {
		if previous, ok := previous.(*commandRequestTransfer); ok {
			previous.stop()
			c.sendCommandComplete(previous.conn, previous.request.ID, http.StatusBadRequest, "duplicate command body transfer ID")
		}
	}
}

func (c *TunnelClient) handleCommandRequestChunk(ctx context.Context, msg *TunnelMessage, workers *sync.WaitGroup) {
	if c == nil || msg == nil {
		return
	}

	value, ok := c.requestTransfers.Load(msg.ID)
	if !ok {
		slog.WarnContext(ctx, "Received command body chunk for unknown transfer", "transferId", msg.ID)
		return
	}
	transfer, ok := value.(*commandRequestTransfer)
	if !ok {
		c.requestTransfers.Delete(msg.ID)
		slog.WarnContext(ctx, "Discarded invalid command body transfer state", "transferId", msg.ID)
		return
	}
	if msg.Sequence != transfer.nextSequence {
		if c.requestTransfers.CompareAndDelete(msg.ID, transfer) {
			transfer.stop()
			c.sendCommandComplete(transfer.conn, transfer.request.ID, http.StatusBadRequest, "command body chunks arrived out of order")
		}
		return
	}

	_, _ = transfer.body.Write(msg.Body)
	transfer.nextSequence++
	if !msg.EOF {
		return
	}

	if c.requestTransfers.CompareAndDelete(msg.ID, transfer) {
		transfer.stop()
		transfer.request.Body = append([]byte(nil), transfer.body.Bytes()...)
		workers.Go(func() { c.handleCommandRequest(ctx, transfer.conn, transfer.request) })
	}
}

func (c *TunnelClient) handleCommandRequest(ctx context.Context, conn TunnelConnection, msg *TunnelMessage) {
	if !ValidateEdgeCommand(msg.Command, msg.Method, msg.Path, false) {
		c.sendCommandComplete(conn, msg.ID, http.StatusBadRequest, "unsupported edge command")
		return
	}
	if conn == nil {
		return
	}
	if err := conn.Send(&TunnelMessage{ID: msg.ID, Type: MessageTypeCommandAck, Command: msg.Command}); err != nil {
		slog.WarnContext(ctx, "Failed to acknowledge edge command", "id", msg.ID, "command", msg.Command, "error", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	req, err := c.buildLocalHTTPRequest(reqCtx, msg)
	if err != nil {
		c.sendCommandComplete(conn, msg.ID, http.StatusInternalServerError, fmt.Sprintf("failed to create request: %v", err))
		return
	}

	recorder := &commandResponseRecorder{
		commandID:   msg.ID,
		commandName: msg.Command,
		conn:        conn,
		headers:     make(http.Header),
		statusCode:  http.StatusOK,
		ctx:         reqCtx,
		credited:    make(chan struct{}, 1),
	}
	if registration, _ := c.registration.Load(); registration.commandCredit {
		recorder.window = commandCreditWindow
		c.commandRecorders.Store(msg.ID, recorder)
		defer c.commandRecorders.Delete(msg.ID)
	}
	c.handler.ServeHTTP(recorder, req)
	if closeErr := recorder.Close(); closeErr != nil {
		slog.WarnContext(reqCtx, "Failed to finalize command response", "id", msg.ID, "command", msg.Command, "error", closeErr)
	}
}

func (c *TunnelClient) buildLocalHTTPRequest(ctx context.Context, msg *TunnelMessage) (*http.Request, error) {
	var body io.Reader
	if len(msg.Body) > 0 {
		body = bytes.NewReader(append([]byte(nil), msg.Body...))
	}

	path := msg.Path + kit.Ternary(msg.Query != "", "?"+msg.Query, "")
	req, err := http.NewRequestWithContext(ctx, msg.Method, path, body)
	if err != nil {
		return nil, err
	}

	// net/http ignores Header.Set("Host", ...); the field must be set explicitly.
	for k, v := range msg.Headers {
		if http.CanonicalHeaderKey(k) == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}

	return req, nil
}

// agentAuthCredentials returns the canonical header/metadata credential
// set every agent transport presents, so the three transports cannot drift.
func agentAuthCredentials(token string) map[string]string {
	return map[string]string{
		HeaderAgentToken:    token,
		HeaderAPIKey:        token,
		HeaderAuthorization: "Bearer " + token,
	}
}

// handleRequest serves a tunneled request locally, streaming the response on gRPC connections.
func (c *TunnelClient) handleRequest(ctx context.Context, conn TunnelConnection, msg *TunnelMessage) {
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	reqCtx = context.WithValue(reqCtx, internalTunnelRequestContextKey{}, true)
	streaming := conn.Transport() == EdgeTransportGRPC

	slog.DebugContext(reqCtx, "Processing tunneled request", "id", msg.ID, "method", msg.Method, "path", msg.Path, "bodyLength", len(msg.Body), "streaming", streaming)

	req, err := c.buildLocalHTTPRequest(reqCtx, msg)
	if err != nil {
		_ = conn.Send(&TunnelMessage{
			ID:     msg.ID,
			Type:   MessageTypeResponse,
			Status: http.StatusInternalServerError,
			Body:   fmt.Appendf(nil, "failed to create request: %v", err),
		})
		return
	}

	if streaming {
		recorder := &streamingResponseRecorder{requestID: msg.ID, conn: conn, headers: make(http.Header), statusCode: http.StatusOK}
		c.handler.ServeHTTP(recorder, req)
		if closeErr := recorder.Close(); closeErr != nil {
			slog.WarnContext(reqCtx, "Failed to finalize streamed response", "id", msg.ID, "error", closeErr)
		}
		return
	}

	rw := &responseRecorder{headers: make(http.Header), statusCode: http.StatusOK}
	c.handler.ServeHTTP(rw, req)
	resp := &TunnelMessage{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Status:  rw.statusCode,
		Headers: flattenResponseHeaders(rw.headers),
		Body:    rw.body.Bytes(),
	}
	if sendErr := conn.Send(resp); sendErr != nil {
		slog.ErrorContext(reqCtx, "Failed to send response", "id", msg.ID, "error", sendErr)
	} else {
		slog.DebugContext(reqCtx, "Sent tunneled response", "id", msg.ID, "status", rw.statusCode)
	}
}

// handleWebSocketStart proxies a manager WebSocket or stream-open request to the local server.
func (c *TunnelClient) handleWebSocketStart(ctx context.Context, conn TunnelConnection, msg *TunnelMessage, workers *sync.WaitGroup) {
	streamID := msg.ID
	if msg.Type == MessageTypeStreamOpen && !ValidateEdgeCommand(msg.Command, http.MethodGet, msg.Path, true) {
		c.sendStreamCloseMessage(conn, streamID, "unsupported edge stream")
		return
	}
	slog.DebugContext(ctx, "Starting WebSocket stream", "streamId", streamID, "path", msg.Path)

	localURL := c.buildLocalWebSocketURL(msg)
	dialCtx, cancelDial := context.WithTimeout(ctx, 30*time.Second)
	defer cancelDial()
	ws, resp, err := websocket.Dial(dialCtx, localURL, &websocket.DialOptions{HTTPHeader: c.buildLocalWebSocketHeaders(msg)})
	// coder/websocket leaves resp.Body nil on a successful handshake.
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		attrs := []any{"error", err, "url", localURL}
		if resp != nil {
			attrs = append(attrs, "status", resp.StatusCode)
			if resp.Body != nil {
				buf := make([]byte, 512)
				if n, _ := resp.Body.Read(buf); n > 0 {
					attrs = append(attrs, "response_body", string(buf[:n]))
				}
			}
		}
		slog.ErrorContext(ctx, "Failed to dial local WebSocket", attrs...)
		c.sendStreamCloseMessage(conn, streamID, "")
		return
	}
	// Local frames are forwarded into tunnel messages, so allow that size instead of the 32KB default.
	ws.SetReadLimit(maxGRPCTunnelMessageSize)

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &activeWSStream{ws: ws, conn: conn, cancel: cancel, dataCh: make(chan wsPayload, 100)}
	c.activeStreams.Store(streamID, stream)

	workers.Go(func() {
		defer c.closeWebSocketStream(streamID, stream)
		for {
			msgType, data, readErr := ws.Read(streamCtx)
			if readErr != nil {
				if !wshub.IsExpectedClose(readErr) {
					slog.DebugContext(ctx, "Local WebSocket read error", "error", readErr)
				}
				c.sendStreamCloseMessage(conn, streamID, "")
				return
			}
			if sendErr := conn.Send(&TunnelMessage{ID: streamID, Type: MessageTypeWebSocketData, Body: data, WSMessageType: int(msgType)}); sendErr != nil {
				slog.DebugContext(ctx, "Failed to send WebSocket data to manager", "error", sendErr)
				return
			}
		}
	})
	workers.Go(func() { c.startLocalWebSocketWriteLoop(ctx, streamCtx, ws, stream, cancel) })
}

func (c *TunnelClient) buildLocalWebSocketURL(msg *TunnelMessage) string {
	// LISTEN may be a host, host:port, IPv6, or bracketed IPv6 with port.
	host := strings.TrimSpace(c.cfg.Listen)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	if host = strings.Trim(host, "[]"); slices.Contains([]string{"", "0.0.0.0", "::"}, host) {
		host = "localhost"
	}
	return "ws://" + net.JoinHostPort(host, c.localPort) + msg.Path + kit.Ternary(msg.Query != "", "?"+msg.Query, "")
}

func (c *TunnelClient) buildLocalWebSocketHeaders(msg *TunnelMessage) http.Header {
	headers := http.Header{}
	for k, v := range msg.Headers {
		if canonicalKey := http.CanonicalHeaderKey(k); !localDialSkipHeaders[canonicalKey] {
			headers.Set(canonicalKey, v)
		}
	}

	if c.cfg.AgentToken != "" {
		headers.Set(HeaderAPIKey, c.cfg.AgentToken)
		headers.Set(HeaderAgentToken, c.cfg.AgentToken)
	}

	return headers
}

func (c *TunnelClient) closeWebSocketStream(streamID string, stream *activeWSStream) {
	stream.mu.Lock()
	if stream.closed {
		stream.mu.Unlock()
		return
	}
	stream.closed = true
	close(stream.dataCh)
	stream.mu.Unlock()

	stream.cancel()
	_ = stream.ws.CloseNow()
	c.activeStreams.Delete(streamID)
}

func (c *TunnelClient) startLocalWebSocketWriteLoop(ctx, streamCtx context.Context, ws *websocket.Conn, stream *activeWSStream, cancel context.CancelFunc) {
	for {
		select {
		case <-streamCtx.Done():
			return
		case payload, ok := <-stream.dataCh:
			if !ok {
				return
			}
			if !isForwardableWSMessage(payload.messageType) {
				slog.WarnContext(ctx, "Dropping WebSocket message with unsupported type", "messageType", payload.messageType)
				continue
			}
			if err := ws.Write(streamCtx, websocket.MessageType(payload.messageType), payload.data); err != nil {
				slog.DebugContext(ctx, "Failed to write to local WebSocket", "error", err)
				cancel()
				return
			}
		}
	}
}

func (c *TunnelClient) sendStreamCloseMessage(conn TunnelConnection, streamID, message string) {
	if conn == nil {
		return
	}
	_ = conn.Send(&TunnelMessage{ID: streamID, Type: MessageTypeStreamClose, Error: message})
}

func (c *TunnelClient) handleStreamData(ctx context.Context, msg *TunnelMessage) {
	streamRaw, ok := c.activeStreams.Load(msg.ID)
	if !ok {
		slog.DebugContext(ctx, "Received WebSocket data for unknown stream", "streamId", msg.ID)
		return
	}
	stream, ok := streamRaw.(*activeWSStream)
	if !ok {
		return
	}
	stream.mu.Lock()
	if stream.closed {
		stream.mu.Unlock()
		return
	}
	select {
	case stream.dataCh <- wsPayload{messageType: msg.WSMessageType, data: msg.Body}:
		stream.mu.Unlock()
	default:
		stream.mu.Unlock()
		// Drop if channel is full (backpressure)
		slog.DebugContext(ctx, "Dropping WebSocket data due to backpressure", "streamId", msg.ID)
	}
}

func (c *TunnelClient) sendCommandComplete(conn TunnelConnection, commandID string, status int, message string) {
	if conn == nil {
		return
	}
	_ = conn.Send(&TunnelMessage{
		ID:     commandID,
		Type:   MessageTypeCommandComplete,
		Status: status,
		Error:  message,
	})
}

func (r *commandResponseRecorder) Header() http.Header {
	return r.headers
}

func (r *commandResponseRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.flushErr != nil {
		return 0, r.flushErr
	}
	if len(b) == 0 {
		return 0, nil
	}

	if !r.streaming && r.buffer.Len()+len(b) <= defaultCommandChunkSize {
		return r.buffer.Write(b)
	}

	r.streaming = true
	if err := r.flushBufferLocked(); err != nil {
		return 0, err
	}
	for chunk := range slices.Chunk(b, defaultCommandChunkSize) {
		if err := r.sendOutputLocked(chunk); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

func (r *commandResponseRecorder) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCode = statusCode
	r.wroteHeader = true
}

func (r *commandResponseRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.streaming = true
	if r.flushErr != nil {
		return
	}
	// An empty first flush still sends the status and headers so the manager can start the response.
	if r.sequence == 0 && r.buffer.Len() == 0 {
		r.flushErr = r.sendOutputLocked(nil)
		return
	}
	r.flushErr = r.flushBufferLocked()
}

func (r *commandResponseRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if r.flushErr != nil {
		return r.flushErr
	}

	complete := &TunnelMessage{
		ID:        r.commandID,
		Type:      MessageTypeCommandComplete,
		Status:    r.statusCode,
		Headers:   flattenResponseHeaders(r.headers),
		Streaming: r.streaming,
		Command:   r.commandName,
	}
	if r.streaming {
		if err := r.flushBufferLocked(); err != nil {
			return err
		}
	} else {
		complete.Body = append([]byte(nil), r.buffer.Bytes()...)
	}
	if err := r.conn.Send(complete); err != nil {
		return err
	}
	r.closed = true
	return nil
}

func (r *commandResponseRecorder) flushBufferLocked() error {
	if r.buffer.Len() == 0 {
		return nil
	}
	if err := r.sendOutputLocked(r.buffer.Bytes()); err != nil {
		return err
	}
	r.buffer.Reset()
	return nil
}

// sendOutputLocked sends one output chunk, first waiting for manager credit when the window is
// full. The first chunk also carries the status and headers so the manager can stream the response.
func (r *commandResponseRecorder) sendOutputLocked(chunk []byte) error {
	for r.window > 0 && r.inFlight.Load() > 0 && r.inFlight.Load()+int64(len(chunk)) > r.window {
		select {
		case <-r.credited:
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
	}
	r.inFlight.Add(int64(len(chunk)))

	msg := &TunnelMessage{
		ID:       r.commandID,
		Type:     MessageTypeCommandOutput,
		Body:     append([]byte(nil), chunk...),
		Sequence: r.sequence,
		Command:  r.commandName,
	}
	if r.sequence == 0 {
		msg.Status = r.statusCode
		msg.Headers = flattenResponseHeaders(r.headers)
	}
	if err := r.conn.Send(msg); err != nil {
		return err
	}
	r.sequence++
	return nil
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

func (r *responseRecorder) Header() http.Header {
	return r.headers
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

func (r *streamingResponseRecorder) Header() http.Header {
	return r.headers
}

func (r *streamingResponseRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.writeHeaderLocked(); err != nil {
		return 0, err
	}
	for chunk := range slices.Chunk(b, defaultCommandChunkSize) {
		if err := r.conn.Send(&TunnelMessage{
			ID:   r.requestID,
			Type: MessageTypeStreamData,
			Body: append([]byte(nil), chunk...),
		}); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

func (r *streamingResponseRecorder) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCode = statusCode
	_ = r.writeHeaderLocked()
}

func (r *streamingResponseRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.writeHeaderLocked()
}

func (r *streamingResponseRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	if err := r.writeHeaderLocked(); err != nil {
		return err
	}
	if err := r.conn.Send(&TunnelMessage{ID: r.requestID, Type: MessageTypeStreamEnd}); err != nil {
		return err
	}
	r.closed = true
	return nil
}

// writeHeaderLocked sends the response status and headers once, marked as a streamed response.
func (r *streamingResponseRecorder) writeHeaderLocked() error {
	if r.wroteHeader {
		return nil
	}
	respHeaders := flattenResponseHeaders(r.headers)
	respHeaders[tunnelStreamHeader] = "1"
	if err := r.conn.Send(&TunnelMessage{
		ID:      r.requestID,
		Type:    MessageTypeResponse,
		Status:  r.statusCode,
		Headers: respHeaders,
	}); err != nil {
		return err
	}
	r.wroteHeader = true
	return nil
}

// StartTunnelClient starts the tunnel client with an owned background worker.
func StartTunnelClient(ctx context.Context, cfg *Config, handler http.Handler) (func(context.Context) error, error) {
	if !cfg.EdgeAgent {
		return nil, errors.New("edge tunnel disabled")
	}
	if UseGRPCEdgeTransport(cfg) && httpx.ManagerGRPCAddr(cfg.ManagerApiUrl) == "" {
		return nil, errors.New("MANAGER_API_URL with a valid host is required for gRPC transport")
	}
	if UseWebSocketEdgeTransport(cfg) && strings.TrimSpace(httpx.ManagerBaseURL(cfg.ManagerApiUrl)) == "" {
		return nil, errors.New("MANAGER_API_URL is required for websocket transport")
	}
	if UsePollEdgeTransport(cfg) && strings.TrimSpace(httpx.ManagerBaseURL(cfg.ManagerApiUrl)) == "" {
		return nil, errors.New("MANAGER_API_URL is required for poll transport")
	}
	if cfg.AgentToken == "" {
		return nil, errors.New("AGENT_TOKEN is required")
	}

	if err := EnsureAgentMTLSAssets(ctx, cfg); err != nil {
		return nil, err
	}
	if err := ValidateAgentMTLSConfig(cfg); err != nil {
		return nil, err
	}

	client := NewTunnelClient(cfg, handler)
	return concurrency.StartSupervised(ctx, "Edge tunnel client", func(runCtx context.Context) error {
		client.StartWithErrorChan(runCtx, nil)
		return nil
	})
}

func (c *TunnelClient) connectAndServeWebSocket(ctx context.Context) error {
	managerWSURL := c.managerWebSocketURL()
	if managerWSURL == "" {
		return errors.New("manager WebSocket URL is empty")
	}
	httpClient, err := NewManagerHTTPClient(c.cfg, 0)
	if err != nil {
		return fmt.Errorf("failed to configure edge websocket TLS: %w", err)
	}

	headers := http.Header{}
	for header, value := range agentAuthCredentials(c.cfg.AgentToken) {
		headers.Set(header, value)
	}

	slog.DebugContext(ctx, "Dialing manager for websocket edge tunnel", "url", managerWSURL)

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
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to connect to manager websocket endpoint (status: %d, body: %s): %w", resp.StatusCode, string(body), err)
		}
		return fmt.Errorf("failed to connect to manager websocket endpoint: %w", err)
	}

	return c.serveTunnelSession(ctx, NewTunnelConn(conn), managerWSURL)
}
