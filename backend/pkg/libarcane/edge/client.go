package edge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// DefaultHeartbeatInterval is how often the client sends heartbeats
	DefaultHeartbeatInterval = 30 * time.Second
	// DefaultWriteTimeout is the timeout for write operations
	DefaultWriteTimeout = 10 * time.Second
	// DefaultRequestTimeout is the timeout for executing local requests
	DefaultRequestTimeout = 5 * time.Minute
	// DefaultGRPCRegistrationTimeout bounds how long the agent waits for the
	// manager to acknowledge gRPC tunnel registration before treating it as a
	// failed transport attempt.
	DefaultGRPCRegistrationTimeout = 10 * time.Second
	// DefaultWebSocketPreferenceTTL keeps websocket as the preferred transport
	// for a short period after a successful auto-mode fallback.
	DefaultWebSocketPreferenceTTL = 2 * time.Minute
)

// activeWSStream tracks an active WebSocket stream on the agent side.
type activeWSStream struct {
	ws     *websocket.Conn
	cancel context.CancelFunc
	dataCh chan wsPayload
	mu     sync.Mutex
	closed bool
}

type wsPayload struct {
	messageType int
	data        []byte
}

// TunnelClient represents the agent-side tunnel client
type TunnelClient struct {
	cfg                     *Config
	handler                 http.Handler
	reconnectInterval       time.Duration
	heartbeatInterval       time.Duration
	grpcRegistrationTimeout time.Duration
	websocketPreferenceTTL  time.Duration
	managerURL              string
	managerGRPCAddr         string
	localPort               string // Port the agent is running on locally
	httpClient              *http.Client
	conn                    TunnelConnection
	stopCh                  chan struct{}
	requestTimeout          time.Duration
	activeStreams           sync.Map // map[string]*activeWSStream
	transportPreferenceMu   sync.RWMutex
	preferWebSocketUntil    time.Time
}

// NewTunnelClient creates a new tunnel client
func NewTunnelClient(cfg *Config, handler http.Handler) *TunnelClient {
	reconnectInterval := time.Duration(cfg.EdgeReconnectInterval) * time.Second
	if reconnectInterval < time.Second {
		reconnectInterval = 5 * time.Second
	}

	managerURL := ""
	if managerBaseURL := strings.TrimRight(cfg.GetManagerBaseURL(), "/"); managerBaseURL != "" {
		// Convert HTTP to WebSocket URL
		managerURL = HTTPToWebSocketURL(managerBaseURL) + "/api/tunnel/connect"
	}
	managerGRPCAddr := cfg.GetManagerGRPCAddr()

	// Get local port for WebSocket dialing
	localPort := cfg.Port
	if localPort == "" {
		localPort = "3552" // Default port
	}

	return &TunnelClient{
		cfg:                     cfg,
		handler:                 handler,
		reconnectInterval:       reconnectInterval,
		heartbeatInterval:       DefaultHeartbeatInterval,
		grpcRegistrationTimeout: DefaultGRPCRegistrationTimeout,
		websocketPreferenceTTL:  DefaultWebSocketPreferenceTTL,
		managerURL:              managerURL,
		managerGRPCAddr:         managerGRPCAddr,
		localPort:               localPort,
		httpClient:              &http.Client{},
		stopCh:                  make(chan struct{}),
		requestTimeout:          DefaultRequestTimeout,
	}
}

// StartWithErrorChan runs the tunnel client and optionally emits connection errors.
func (c *TunnelClient) StartWithErrorChan(ctx context.Context, errCh chan error) {
	transport := NormalizeEdgeTransport(c.cfg.EdgeTransport)
	slog.InfoContext(ctx, "Starting edge tunnel client",
		"transport_mode", transport,
		"attempt_grpc", c.shouldAttemptGRPCTunnelInternal(),
		"attempt_websocket", c.shouldAttemptWebSocketTunnelInternal(),
		"manager_url", c.managerURL,
	)
	if errCh != nil {
		defer close(errCh)
	}

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "Edge tunnel client shutting down")
			return
		case <-c.stopCh:
			slog.InfoContext(ctx, "Edge tunnel client stopped")
			return
		default:
			if err := c.connectAndServe(ctx); err != nil {
				if errCh != nil {
					select {
					case errCh <- err:
					default:
					}
				} else {
					slog.WarnContext(ctx, "Edge tunnel disconnected", "error", err)
				}
			}

			// Wait before reconnecting
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-time.After(c.reconnectInterval):
				slog.InfoContext(ctx, "Attempting to reconnect edge tunnel")
			}
		}
	}
}

// connectAndServe establishes a connection and handles messages.
func (c *TunnelClient) connectAndServe(ctx context.Context) error {
	if UsePollEdgeTransport(c.cfg) {
		return c.connectAndServePoll(ctx)
	}
	return c.connectAndServeManagedTunnelInternal(ctx)
}

func (c *TunnelClient) connectAndServeManagedTunnelInternal(ctx context.Context) error {
	if c.shouldAttemptGRPCTunnelInternal() {
		if preferredUntil, ok := c.preferredWebSocketUntilInternal(time.Now()); ok {
			slog.InfoContext(ctx, "Temporarily preferring websocket edge tunnel transport after recent websocket success",
				"preferred_until", preferredUntil,
				"manager_ws_url", c.managerWebSocketURLInternal(),
			)
			return c.connectAndServeWebSocket(ctx)
		}

		if err := c.connectAndServeGRPC(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if c.shouldFallbackToWebSocketInternal() {
				managerWSURL := c.managerWebSocketURLInternal()
				slog.WarnContext(ctx, "gRPC edge tunnel connection failed, falling back to websocket transport",
					"error", err,
					"manager_grpc_addr", c.managerGRPCAddr,
					"manager_ws_url", managerWSURL,
				)
				if wsErr := c.connectAndServeWebSocket(ctx); wsErr != nil {
					return fmt.Errorf("gRPC edge tunnel failed: %w; websocket fallback failed: %w", err, wsErr)
				}
				return nil
			}
			return err
		}
		return nil
	}
	if c.shouldAttemptWebSocketTunnelInternal() {
		return c.connectAndServeWebSocket(ctx)
	}
	return fmt.Errorf("no edge tunnel transport is available")
}

func (c *TunnelClient) shouldFallbackToWebSocketInternal() bool {
	if !c.shouldAttemptWebSocketTunnelInternal() {
		return false
	}
	return c.managerWebSocketURLInternal() != ""
}

func (c *TunnelClient) shouldAttemptGRPCTunnelInternal() bool {
	if c == nil {
		return false
	}
	if UsePollEdgeTransport(c.cfg) {
		return strings.TrimSpace(c.managerGRPCAddr) != ""
	}
	return UseGRPCEdgeTransport(c.cfg)
}

func (c *TunnelClient) shouldAttemptWebSocketTunnelInternal() bool {
	if c == nil {
		return false
	}
	if UsePollEdgeTransport(c.cfg) {
		return c.managerWebSocketURLInternal() != ""
	}
	return UseWebSocketEdgeTransport(c.cfg)
}

func (c *TunnelClient) managerWebSocketURLInternal() string {
	if c == nil {
		return ""
	}
	if managerURL := strings.TrimSpace(c.managerURL); managerURL != "" {
		return managerURL
	}
	if c.cfg == nil {
		return ""
	}
	managerBaseURL := strings.TrimRight(strings.TrimSpace(c.cfg.GetManagerBaseURL()), "/")
	if managerBaseURL == "" {
		return ""
	}
	return HTTPToWebSocketURL(managerBaseURL) + "/api/tunnel/connect"
}

func (c *TunnelClient) grpcRegistrationTimeoutInternal() time.Duration {
	if c == nil || c.grpcRegistrationTimeout <= 0 {
		return DefaultGRPCRegistrationTimeout
	}
	return c.grpcRegistrationTimeout
}

func (c *TunnelClient) websocketPreferenceTTLInternal() time.Duration {
	if c == nil || c.websocketPreferenceTTL <= 0 {
		return DefaultWebSocketPreferenceTTL
	}
	return c.websocketPreferenceTTL
}

func (c *TunnelClient) preferredWebSocketUntilInternal(now time.Time) (time.Time, bool) {
	if c == nil || !c.shouldAttemptGRPCTunnelInternal() || !c.shouldAttemptWebSocketTunnelInternal() {
		return time.Time{}, false
	}

	c.transportPreferenceMu.RLock()
	defer c.transportPreferenceMu.RUnlock()

	if c.preferWebSocketUntil.IsZero() || !now.Before(c.preferWebSocketUntil) {
		return time.Time{}, false
	}

	return c.preferWebSocketUntil, true
}

func (c *TunnelClient) markTransportConnectedInternal(transport string) {
	if c == nil {
		return
	}

	c.transportPreferenceMu.Lock()
	defer c.transportPreferenceMu.Unlock()

	switch transport {
	case EdgeTransportGRPC:
		c.preferWebSocketUntil = time.Time{}
	case EdgeTransportWebSocket:
		if c.shouldAttemptGRPCTunnelInternal() && c.shouldAttemptWebSocketTunnelInternal() {
			c.preferWebSocketUntil = time.Now().Add(c.websocketPreferenceTTLInternal())
		}
	}
}

// heartbeatLoop sends periodic heartbeats
func (c *TunnelClient) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.conn == nil || c.conn.IsClosed() {
				return
			}

			msg := &TunnelMessage{
				ID:   uuid.New().String(),
				Type: MessageTypeHeartbeat,
			}

			if err := c.conn.Send(msg); err != nil {
				slog.WarnContext(ctx, "Failed to send heartbeat", "error", err)
				// Force reconnect so the manager does not keep stale state without heartbeats.
				if closeErr := c.conn.Close(); closeErr != nil {
					slog.DebugContext(ctx, "Failed to close tunnel connection after heartbeat failure", "error", closeErr)
				}
				return
			}
			slog.DebugContext(ctx, "Sent heartbeat to manager")
		}
	}
}

// messageLoop processes incoming messages from the manager
func (c *TunnelClient) messageLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := c.conn.Receive()
			if err != nil {
				return fmt.Errorf("failed to receive message: %w", err)
			}

			switch msg.Type {
			case MessageTypeRequest:
				go c.handleRequest(ctx, msg)
			case MessageTypeWebSocketStart:
				c.handleWebSocketStart(ctx, msg)
			case MessageTypeWebSocketData:
				c.handleWebSocketData(ctx, msg)
			case MessageTypeWebSocketClose:
				c.handleWebSocketClose(ctx, msg)
			case MessageTypeResponse, MessageTypeHeartbeat, MessageTypeStreamData, MessageTypeStreamEnd, MessageTypeEvent:
				slog.DebugContext(ctx, "Ignoring message type on agent", "type", msg.Type)
			case MessageTypeHeartbeatAck:
				slog.DebugContext(ctx, "Received heartbeat ack")
			case MessageTypeRegisterResponse:
				if !msg.Accepted {
					return fmt.Errorf("manager rejected tunnel registration: %s", msg.Error)
				}
				slog.InfoContext(ctx, "Edge gRPC tunnel connected to manager",
					"manager_addr", c.managerGRPCAddr,
					"environment_id", msg.EnvironmentID,
				)
			case MessageTypeRegister:
				slog.DebugContext(ctx, "Ignoring register message on agent")
			default:
				slog.WarnContext(ctx, "Unknown message type", "type", msg.Type)
			}
		}
	}
}

// handleRequest processes an incoming request and sends back a response
func (c *TunnelClient) handleRequest(ctx context.Context, msg *TunnelMessage) {
	if c.isGRPCConnectionInternal() {
		c.handleRequestStreaming(ctx, msg)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	slog.DebugContext(reqCtx, "Processing tunneled request", "id", msg.ID, "method", msg.Method, "path", msg.Path, "bodyLength", len(msg.Body))

	// Build the request
	var body io.Reader
	var bodyBytes []byte
	if len(msg.Body) > 0 {
		bodyBytes = msg.Body
		body = bytes.NewReader(bodyBytes)
	}

	path := msg.Path
	if msg.Query != "" {
		path = path + "?" + msg.Query
	}

	req, err := http.NewRequestWithContext(reqCtx, msg.Method, path, body)
	if err != nil {
		c.sendErrorResponse(msg.ID, http.StatusInternalServerError, fmt.Sprintf("failed to create request: %v", err))
		return
	}

	if bodyBytes != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	// Set headers. Note: Go's net/http does not populate req.Host from
	// Header.Set("Host", ...) — it must be set explicitly on the field.
	for k, v := range msg.Headers {
		if http.CanonicalHeaderKey(k) == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}

	// Use a response recorder to capture the response
	rw := &responseRecorder{
		headers:    make(http.Header),
		statusCode: http.StatusOK,
	}

	// Execute the request through the local handler
	c.handler.ServeHTTP(rw, req)

	// Build response message
	respHeaders := make(map[string]string)
	for k, v := range rw.headers {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	resp := &TunnelMessage{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Status:  rw.statusCode,
		Headers: respHeaders,
		Body:    rw.body.Bytes(),
	}

	if err := c.conn.Send(resp); err != nil {
		slog.ErrorContext(reqCtx, "Failed to send response", "id", msg.ID, "error", err)
	} else {
		slog.DebugContext(reqCtx, "Sent tunneled response", "id", msg.ID, "status", rw.statusCode)
	}
}

func (c *TunnelClient) isGRPCConnectionInternal() bool {
	if c == nil || c.conn == nil {
		return false
	}
	_, isGRPC := c.conn.(*GRPCAgentTunnelConn)
	return isGRPC
}

func (c *TunnelClient) handleRequestStreaming(ctx context.Context, msg *TunnelMessage) {
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	slog.DebugContext(reqCtx, "Processing tunneled request (streaming)", "id", msg.ID, "method", msg.Method, "path", msg.Path, "bodyLength", len(msg.Body))

	var body io.Reader
	var bodyBytes []byte
	if len(msg.Body) > 0 {
		bodyBytes = msg.Body
		body = bytes.NewReader(bodyBytes)
	}

	path := msg.Path
	if msg.Query != "" {
		path = path + "?" + msg.Query
	}

	req, err := http.NewRequestWithContext(reqCtx, msg.Method, path, body)
	if err != nil {
		c.sendErrorResponse(msg.ID, http.StatusInternalServerError, fmt.Sprintf("failed to create request: %v", err))
		return
	}

	if bodyBytes != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	// Set headers. Note: Go's net/http does not populate req.Host from
	// Header.Set("Host", ...) — it must be set explicitly on the field.
	for k, v := range msg.Headers {
		if http.CanonicalHeaderKey(k) == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}

	recorder := newStreamingResponseRecorder(msg.ID, c.conn)
	c.handler.ServeHTTP(recorder, req)

	if err := recorder.Close(); err != nil {
		slog.WarnContext(reqCtx, "Failed to finalize streamed response", "id", msg.ID, "error", err)
	}
}

// handleWebSocketStart handles a WebSocket stream start request from the manager.
func (c *TunnelClient) handleWebSocketStart(ctx context.Context, msg *TunnelMessage) {
	streamID := msg.ID
	slog.DebugContext(ctx, "Starting WebSocket stream", "stream_id", streamID, "path", msg.Path)

	localURL := c.buildLocalWebSocketURLInternal(msg)
	headers := c.buildLocalWebSocketHeadersInternal(msg)

	ws, resp, err := c.dialLocalWebSocket(ctx, localURL, headers)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		attrs := []any{"error", err, "url", localURL}
		if resp != nil {
			attrs = append(attrs, "status", resp.StatusCode)
			if resp.Body != nil {
				buf := make([]byte, 512)
				n, _ := resp.Body.Read(buf)
				if n > 0 {
					attrs = append(attrs, "response_body", string(buf[:n]))
				}
			}
		}
		slog.ErrorContext(ctx, "Failed to dial local WebSocket", attrs...)
		c.sendWebSocketClose(streamID)
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := c.registerStream(streamID, ws, cancel)

	go c.startLocalWebSocketReadLoop(ctx, streamCtx, streamID, ws, stream)
	go c.startLocalWebSocketWriteLoop(ctx, streamCtx, ws, stream, cancel)
}

func (c *TunnelClient) buildLocalWebSocketURLInternal(msg *TunnelMessage) string {
	path := msg.Path
	if msg.Query != "" {
		path = path + "?" + msg.Query
	}
	host := c.localWebSocketHostInternal()
	return "ws://" + net.JoinHostPort(host, c.localPort) + path
}

func (c *TunnelClient) localWebSocketHostInternal() string {
	listenHost := strings.TrimSpace(c.cfg.Listen)
	if listenHost == "" {
		return "localhost"
	}

	// LISTEN may be just a host ("0.0.0.0"), host:port ("0.0.0.0:3552"),
	// IPv6 ("::"), or bracketed IPv6 with port ("[::]:3552").
	if strings.HasPrefix(listenHost, ":") {
		return "localhost"
	}
	if host, _, err := net.SplitHostPort(listenHost); err == nil {
		listenHost = host
	}

	trimmed := strings.Trim(listenHost, "[]")

	switch trimmed {
	case "", "0.0.0.0", "::":
		return "localhost"
	default:
		return trimmed
	}
}

// localDialSkipHeaders lists headers that must not be forwarded when the
// agent dials its own local HTTP server for a proxied WebSocket stream.
// This includes:
//   - Standard WebSocket handshake headers (gorilla/websocket sets its own)
//   - Browser-specific headers that were forwarded through the tunnel from
//     the manager.  These cause handshake failures because the agent's
//     WebSocket upgrader validates the Origin against localhost, not the
//     browser's remote origin.
var localDialSkipHeaders = map[string]bool{
	// WebSocket handshake (gorilla/websocket adds its own)
	"Sec-Websocket-Key":        true,
	"Sec-Websocket-Version":    true,
	"Sec-Websocket-Extensions": true,
	"Upgrade":                  true,
	"Connection":               true,

	// Browser headers forwarded through the tunnel that are invalid
	// for a server-to-server local dial.
	"Origin":             true,
	"Cookie":             true,
	"Authorization":      true,
	"Referer":            true,
	"Sec-Fetch-Dest":     true,
	"Sec-Fetch-Mode":     true,
	"Sec-Fetch-Site":     true,
	"Sec-Fetch-User":     true,
	"Sec-Ch-Ua":          true,
	"Sec-Ch-Ua-Mobile":   true,
	"Sec-Ch-Ua-Platform": true,
}

func (c *TunnelClient) buildLocalWebSocketHeadersInternal(msg *TunnelMessage) http.Header {
	headers := http.Header{}
	for k, v := range msg.Headers {
		canonicalKey := http.CanonicalHeaderKey(k)
		if !localDialSkipHeaders[canonicalKey] {
			headers.Set(canonicalKey, v)
		}
	}

	if c.cfg.AgentToken != "" {
		headers.Set(HeaderAPIKey, c.cfg.AgentToken)
		headers.Set(HeaderAgentToken, c.cfg.AgentToken)
	}

	return headers
}

func (c *TunnelClient) dialLocalWebSocket(ctx context.Context, localURL string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}

	return dialer.DialContext(ctx, localURL, headers)
}

func (c *TunnelClient) registerStream(streamID string, ws *websocket.Conn, cancel context.CancelFunc) *activeWSStream {
	stream := &activeWSStream{
		ws:     ws,
		cancel: cancel,
		dataCh: make(chan wsPayload, 100),
	}
	c.activeStreams.Store(streamID, stream)
	return stream
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
	_ = stream.ws.Close()
	c.activeStreams.Delete(streamID)
}

func (c *TunnelClient) startLocalWebSocketReadLoop(ctx context.Context, streamCtx context.Context, streamID string, ws *websocket.Conn, stream *activeWSStream) {
	defer func() {
		c.closeWebSocketStream(streamID, stream)
	}()

	for {
		if streamCtx.Err() != nil {
			return
		}

		msgType, data, err := ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived) {
				slog.DebugContext(ctx, "Local WebSocket read error", "error", err)
			}
			c.sendWebSocketClose(streamID)
			return
		}

		if err := c.sendWebSocketData(streamID, msgType, data); err != nil {
			slog.DebugContext(ctx, "Failed to send WebSocket data to manager", "error", err)
			return
		}
	}
}

func (c *TunnelClient) startLocalWebSocketWriteLoop(ctx context.Context, streamCtx context.Context, ws *websocket.Conn, stream *activeWSStream, cancel context.CancelFunc) {
	for {
		select {
		case <-streamCtx.Done():
			return
		case payload, ok := <-stream.dataCh:
			if !ok {
				return
			}
			msgType := payload.messageType
			if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
				slog.WarnContext(ctx, "Dropping WebSocket message with unsupported type", "messageType", msgType)
				continue
			}
			if err := ws.WriteMessage(msgType, payload.data); err != nil {
				slog.DebugContext(ctx, "Failed to write to local WebSocket", "error", err)
				cancel()
				return
			}
		}
	}
}

func (c *TunnelClient) sendWebSocketData(streamID string, msgType int, data []byte) error {
	wsDataMsg := &TunnelMessage{
		ID:            streamID,
		Type:          MessageTypeWebSocketData,
		Body:          data,
		WSMessageType: msgType,
	}
	return c.conn.Send(wsDataMsg)
}

func (c *TunnelClient) sendWebSocketClose(streamID string) {
	closeMsg := &TunnelMessage{
		ID:   streamID,
		Type: MessageTypeWebSocketClose,
	}
	_ = c.conn.Send(closeMsg)
}

// handleWebSocketData handles incoming WebSocket data from the manager.
func (c *TunnelClient) handleWebSocketData(ctx context.Context, msg *TunnelMessage) {
	streamRaw, ok := c.activeStreams.Load(msg.ID)
	if !ok {
		slog.DebugContext(ctx, "Received WebSocket data for unknown stream", "stream_id", msg.ID)
		return
	}
	stream := streamRaw.(*activeWSStream)
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
		slog.DebugContext(ctx, "Dropping WebSocket data due to backpressure", "stream_id", msg.ID)
	}
}

// handleWebSocketClose handles WebSocket close from the manager.
func (c *TunnelClient) handleWebSocketClose(ctx context.Context, msg *TunnelMessage) {
	streamRaw, ok := c.activeStreams.Load(msg.ID)
	if !ok {
		return
	}
	stream := streamRaw.(*activeWSStream)
	c.closeWebSocketStream(msg.ID, stream)
	slog.DebugContext(ctx, "Closed WebSocket stream", "stream_id", msg.ID)
}

// sendErrorResponse sends an error response
func (c *TunnelClient) sendErrorResponse(requestID string, status int, message string) {
	resp := &TunnelMessage{
		ID:     requestID,
		Type:   MessageTypeResponse,
		Status: status,
		Body:   []byte(message),
	}
	_ = c.conn.Send(resp)
}

// responseRecorder captures HTTP responses
type responseRecorder struct {
	headers    http.Header
	body       bytes.Buffer
	statusCode int
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

type streamingResponseRecorder struct {
	requestID   string
	conn        TunnelConnection
	headers     http.Header
	statusCode  int
	wroteHeader bool
	closed      bool
	mu          sync.Mutex
}

func newStreamingResponseRecorder(requestID string, conn TunnelConnection) *streamingResponseRecorder {
	return &streamingResponseRecorder{
		requestID:  requestID,
		conn:       conn,
		headers:    make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (r *streamingResponseRecorder) Header() http.Header {
	return r.headers
}

func (r *streamingResponseRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.wroteHeader {
		if err := r.writeHeaderLocked(r.statusCode); err != nil {
			return 0, err
		}
	}

	if len(b) == 0 {
		return 0, nil
	}

	if err := r.conn.Send(&TunnelMessage{
		ID:   r.requestID,
		Type: MessageTypeStreamData,
		Body: append([]byte(nil), b...),
	}); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (r *streamingResponseRecorder) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.statusCode = statusCode
	if r.wroteHeader {
		return
	}
	if err := r.writeHeaderLocked(statusCode); err != nil {
		return
	}
}

func (r *streamingResponseRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.wroteHeader {
		_ = r.writeHeaderLocked(r.statusCode)
	}
}

func (r *streamingResponseRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	if !r.wroteHeader {
		if err := r.writeHeaderLocked(r.statusCode); err != nil {
			return err
		}
	}

	if err := r.conn.Send(&TunnelMessage{
		ID:   r.requestID,
		Type: MessageTypeStreamEnd,
	}); err != nil {
		return err
	}

	r.closed = true
	return nil
}

func (r *streamingResponseRecorder) writeHeaderLocked(statusCode int) error {
	respHeaders := make(map[string]string)
	for k, v := range r.headers {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}
	respHeaders["X-Arcane-Tunnel-Stream"] = "1"

	if err := r.conn.Send(&TunnelMessage{
		ID:      r.requestID,
		Type:    MessageTypeResponse,
		Status:  statusCode,
		Headers: respHeaders,
	}); err != nil {
		return err
	}
	r.wroteHeader = true
	return nil
}

// StartTunnelClientWithErrors starts the tunnel client and returns a channel for connection errors.
func StartTunnelClientWithErrors(ctx context.Context, cfg *Config, handler http.Handler) (<-chan error, error) {
	if !cfg.EdgeAgent {
		return nil, fmt.Errorf("edge tunnel disabled")
	}

	if UseGRPCEdgeTransport(cfg) {
		if cfg.GetManagerGRPCAddr() == "" {
			return nil, fmt.Errorf("MANAGER_API_URL with a valid host is required for gRPC transport")
		}
	}

	if UseWebSocketEdgeTransport(cfg) && strings.TrimSpace(cfg.GetManagerBaseURL()) == "" {
		return nil, fmt.Errorf("MANAGER_API_URL is required for websocket transport")
	}

	if UsePollEdgeTransport(cfg) && strings.TrimSpace(cfg.GetManagerBaseURL()) == "" {
		return nil, fmt.Errorf("MANAGER_API_URL is required for poll transport")
	}

	if cfg.AgentToken == "" {
		return nil, fmt.Errorf("AGENT_TOKEN is required")
	}

	client := NewTunnelClient(cfg, handler)
	errCh := make(chan error, 1)
	go client.StartWithErrorChan(ctx, errCh)
	return errCh, nil
}
