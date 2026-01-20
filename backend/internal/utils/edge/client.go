package edge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
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
)

// activeWSStream tracks an active WebSocket stream on the agent side
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
	cfg               *config.Config
	handler           http.Handler
	reconnectInterval time.Duration
	heartbeatInterval time.Duration
	managerURL        string
	localPort         string // Port the agent is running on locally
	conn              *TunnelConn
	stopCh            chan struct{}
	requestTimeout    time.Duration
	activeStreams     sync.Map // map[string]*activeWSStream
}

// NewTunnelClient creates a new tunnel client
func NewTunnelClient(cfg *config.Config, handler http.Handler) *TunnelClient {
	reconnectInterval := time.Duration(cfg.EdgeReconnectInterval) * time.Second
	if reconnectInterval < time.Second {
		reconnectInterval = 5 * time.Second
	}

	managerURL := strings.TrimRight(cfg.GetManagerBaseURL(), "/")
	// Convert HTTP to WebSocket URL
	managerURL = remenv.HTTPToWebSocketURL(managerURL) + "/api/tunnel/connect"

	// Get local port for WebSocket dialing
	localPort := cfg.Port
	if localPort == "" {
		localPort = "3552" // Default port
	}

	return &TunnelClient{
		cfg:               cfg,
		handler:           handler,
		reconnectInterval: reconnectInterval,
		heartbeatInterval: DefaultHeartbeatInterval,
		managerURL:        managerURL,
		localPort:         localPort,
		stopCh:            make(chan struct{}),
		requestTimeout:    DefaultRequestTimeout,
	}
}

// Start begins the tunnel client, attempting to connect and reconnect as needed
func (c *TunnelClient) Start(ctx context.Context) {
	c.StartWithErrorChan(ctx, nil)
}

// StartWithErrorChan runs the tunnel client and optionally emits connection errors.
func (c *TunnelClient) StartWithErrorChan(ctx context.Context, errCh chan error) {
	slog.InfoContext(ctx, "Starting edge tunnel client", "manager_url", c.managerURL)
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

// connectAndServe establishes a connection and handles messages
func (c *TunnelClient) connectAndServe(ctx context.Context) error {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 30 * time.Second,
	}

	// Set up headers with agent token
	headers := http.Header{}
	headers.Set(remenv.HeaderAgentToken, c.cfg.AgentToken)
	headers.Set(remenv.HeaderAPIKey, c.cfg.AgentToken)

	slog.DebugContext(ctx, "Dialing manager for edge tunnel", "url", c.managerURL)

	conn, resp, err := dialer.DialContext(ctx, c.managerURL, headers)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to connect to manager: %w, status: %d, body: %s", err, resp.StatusCode, string(body))
		}
		return fmt.Errorf("failed to connect to manager: %w", err)
	}
	defer conn.Close()

	c.conn = NewTunnelConn(conn)
	slog.InfoContext(ctx, "Edge tunnel connected to manager")

	// Start heartbeat goroutine
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go c.heartbeatLoop(heartbeatCtx)

	// Process incoming messages
	return c.messageLoop(ctx)
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
				go c.handleWebSocketStart(ctx, msg)
			case MessageTypeWebSocketData:
				c.handleWebSocketData(ctx, msg)
			case MessageTypeWebSocketClose:
				c.handleWebSocketClose(ctx, msg)
			case MessageTypeResponse, MessageTypeHeartbeat, MessageTypeStreamData, MessageTypeStreamEnd:
				slog.DebugContext(ctx, "Ignoring message type on agent", "type", msg.Type)
			case MessageTypeHeartbeatAck:
				slog.DebugContext(ctx, "Received heartbeat ack")
			default:
				slog.WarnContext(ctx, "Unknown message type", "type", msg.Type)
			}
		}
	}
}

// handleRequest processes an incoming request and sends back a response
func (c *TunnelClient) handleRequest(ctx context.Context, msg *TunnelMessage) {
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	slog.DebugContext(reqCtx, "Processing tunneled request", "id", msg.ID, "method", msg.Method, "path", msg.Path)

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

	// Set headers
	for k, v := range msg.Headers {
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

// handleWebSocketStart handles a WebSocket stream start request from the manager
func (c *TunnelClient) handleWebSocketStart(ctx context.Context, msg *TunnelMessage) {
	streamID := msg.ID
	slog.DebugContext(ctx, "Starting WebSocket stream", "stream_id", streamID, "path", msg.Path)

	localURL := c.buildLocalWebSocketURL(msg)
	headers := c.buildLocalWebSocketHeaders(msg)

	ws, resp, err := c.dialLocalWebSocket(ctx, localURL, headers)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		slog.ErrorContext(ctx, "Failed to dial local WebSocket", "error", err, "url", localURL)
		c.sendWebSocketClose(streamID)
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := c.registerStream(streamID, ws, cancel)

	go c.startLocalWebSocketReadLoop(ctx, streamCtx, streamID, ws, stream)
	go c.startLocalWebSocketWriteLoop(ctx, streamCtx, ws, stream, cancel)
}

func (c *TunnelClient) buildLocalWebSocketURL(msg *TunnelMessage) string {
	path := msg.Path
	if msg.Query != "" {
		path = path + "?" + msg.Query
	}
	return "ws://localhost:" + c.localPort + path
}

func (c *TunnelClient) buildLocalWebSocketHeaders(msg *TunnelMessage) http.Header {
	headers := http.Header{}
	wsHandshakeHeaders := map[string]bool{
		"Sec-Websocket-Key":        true,
		"Sec-Websocket-Version":    true,
		"Sec-Websocket-Extensions": true,
		"Sec-Websocket-Protocol":   true,
		"Upgrade":                  true,
		"Connection":               true,
	}
	for k, v := range msg.Headers {
		if !wsHandshakeHeaders[k] {
			headers.Set(k, v)
		}
	}

	if c.cfg.AgentToken != "" {
		headers.Set(remenv.HeaderAPIKey, c.cfg.AgentToken)
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

// handleWebSocketData handles incoming WebSocket data from the manager

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

// handleWebSocketClose handles WebSocket close from the manager
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

// StartTunnelClient starts the edge tunnel client in a goroutine
// This is called from bootstrap when in edge mode
func StartTunnelClient(ctx context.Context, cfg *config.Config, handler http.Handler) {
	if !cfg.EdgeAgent {
		slog.WarnContext(ctx, "StartTunnelClient called but EDGE_AGENT is disabled")
		return
	}

	if cfg.ManagerApiUrl == "" {
		slog.ErrorContext(ctx, "Cannot start edge tunnel: MANAGER_API_URL is required")
		return
	}

	if cfg.AgentToken == "" {
		slog.ErrorContext(ctx, "Cannot start edge tunnel: AGENT_TOKEN is required")
		return
	}

	client := NewTunnelClient(cfg, handler)
	// Run in background - this will auto-reconnect
	go client.Start(ctx)
}

// StartTunnelClientWithErrors starts the tunnel client and returns a channel for connection errors.
func StartTunnelClientWithErrors(ctx context.Context, cfg *config.Config, handler http.Handler) (<-chan error, error) {
	if !cfg.EdgeAgent {
		return nil, fmt.Errorf("edge tunnel disabled")
	}

	if cfg.ManagerApiUrl == "" {
		return nil, fmt.Errorf("MANAGER_API_URL is required")
	}

	if cfg.AgentToken == "" {
		return nil, fmt.Errorf("AGENT_TOKEN is required")
	}

	client := NewTunnelClient(cfg, handler)
	errCh := make(chan error, 1)
	go client.StartWithErrorChan(ctx, errCh)
	return errCh, nil
}
