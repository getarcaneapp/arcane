package edge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TunnelMessageType represents the type of message sent over the tunnel.
type TunnelMessageType string

const (
	// MessageTypeRequest is sent from manager to agent to initiate a request.
	MessageTypeRequest TunnelMessageType = "request"
	// MessageTypeResponse is sent from agent to manager with the response.
	MessageTypeResponse TunnelMessageType = "response"
	// MessageTypeHeartbeat is sent by agents to keep the connection alive.
	MessageTypeHeartbeat TunnelMessageType = "heartbeat"
	// MessageTypeHeartbeatAck is sent by manager to acknowledge a heartbeat.
	MessageTypeHeartbeatAck TunnelMessageType = "heartbeat_ack"
	// MessageTypeStreamData is sent for streaming responses (logs, stats).
	MessageTypeStreamData TunnelMessageType = "stream_data"
	// MessageTypeStreamEnd indicates end of a stream.
	MessageTypeStreamEnd TunnelMessageType = "stream_end"
	// MessageTypeWebSocketStart starts a WebSocket stream for logs/stats.
	MessageTypeWebSocketStart TunnelMessageType = "ws_start"
	// MessageTypeWebSocketData is a WebSocket message in either direction.
	MessageTypeWebSocketData TunnelMessageType = "ws_data"
	// MessageTypeWebSocketClose closes a WebSocket stream.
	MessageTypeWebSocketClose TunnelMessageType = "ws_close"
	// MessageTypeRegister is the first message sent by the agent on gRPC transport.
	MessageTypeRegister TunnelMessageType = "register"
	// MessageTypeRegisterResponse is sent by manager after register validation.
	MessageTypeRegisterResponse TunnelMessageType = "register_response"
	// MessageTypeEvent carries an event emitted by an agent to the manager.
	MessageTypeEvent TunnelMessageType = "event"
)

// TunnelMessage represents a transport-agnostic edge tunnel message.
type TunnelMessage struct {
	ID            string            `json:"id"`                        // Unique request/stream ID
	Type          TunnelMessageType `json:"type"`                      // Message type
	Method        string            `json:"method,omitempty"`          // HTTP method for requests
	Path          string            `json:"path,omitempty"`            // Request path
	Query         string            `json:"query,omitempty"`           // Query string
	Headers       map[string]string `json:"headers,omitempty"`         // HTTP headers
	Body          []byte            `json:"body,omitempty"`            // Request/response body
	WSMessageType int               `json:"ws_message_type,omitempty"` // WebSocket message type
	Status        int               `json:"status,omitempty"`          // HTTP status for responses
	Accepted      bool              `json:"accepted,omitempty"`        // Registration accepted
	AgentToken    string            `json:"agent_token,omitempty"`     // Register request token
	EnvironmentID string            `json:"environment_id,omitempty"`  // Manager-resolved environment ID
	Error         string            `json:"error,omitempty"`           // Error field for register response
	Event         *TunnelEvent      `json:"event,omitempty"`           // Agent event payload
}

// TunnelEvent is an event payload sent from an agent to the manager.
type TunnelEvent struct {
	Type         string `json:"type"`
	Severity     string `json:"severity,omitempty"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Username     string `json:"username,omitempty"`
	MetadataJSON []byte `json:"metadata_json,omitempty"`
}

// MarshalJSON custom marshaler to handle nil body as empty.
func (m *TunnelMessage) MarshalJSON() ([]byte, error) {
	type Alias TunnelMessage
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// PendingRequest tracks an in-flight request waiting for response.
type PendingRequest struct {
	ResponseCh chan *TunnelMessage
	CreatedAt  time.Time
}

// TunnelConnection is the transport contract shared by WebSocket and gRPC wrappers.
type TunnelConnection interface {
	Send(msg *TunnelMessage) error
	Receive() (*TunnelMessage, error)
	IsExpectedReceiveError(err error) bool
	Close() error
	IsClosed() bool
	SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error)
}

const defaultSendRequestTimeout = 5 * time.Minute

// TunnelConn wraps a WebSocket connection with send/receive helpers.
type TunnelConn struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	closed   bool
	closedMu sync.RWMutex
}

// NewTunnelConn creates a new WebSocket tunnel connection wrapper.
func NewTunnelConn(conn *websocket.Conn) *TunnelConn {
	return &TunnelConn{conn: conn}
}

// Send sends a tunnel message over the WebSocket connection.
func (t *TunnelConn) Send(msg *TunnelMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closedMu.RLock()
	if t.closed {
		t.closedMu.RUnlock()
		return websocket.ErrCloseSent
	}
	t.closedMu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return t.conn.WriteMessage(websocket.TextMessage, data)
}

// Receive receives a tunnel message from the WebSocket connection.
func (t *TunnelConn) Receive() (*TunnelMessage, error) {
	_, data, err := t.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	var msg TunnelMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// IsExpectedReceiveError returns true for normal WebSocket close/teardown errors.
func (t *TunnelConn) IsExpectedReceiveError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

// Close closes the WebSocket tunnel connection.
func (t *TunnelConn) Close() error {
	t.closedMu.Lock()
	t.closed = true
	t.closedMu.Unlock()

	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Close()
}

// IsClosed returns whether the connection is closed.
func (t *TunnelConn) IsClosed() bool {
	t.closedMu.RLock()
	defer t.closedMu.RUnlock()
	return t.closed
}

// SendRequest sends a request and waits for response.
func (t *TunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return sendRequestWithPending(ctx, t, msg, pending)
}

type grpcManagerStream interface {
	Send(*tunnelpb.ManagerMessage) error
	Recv() (*tunnelpb.AgentMessage, error)
	Context() context.Context
}

type grpcAgentStream interface {
	Send(*tunnelpb.AgentMessage) error
	Recv() (*tunnelpb.ManagerMessage, error)
	Context() context.Context
	CloseSend() error
}

// GRPCManagerTunnelConn wraps the manager-side gRPC tunnel stream.
type GRPCManagerTunnelConn struct {
	stream   grpcManagerStream
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	closedMu sync.RWMutex
}

// NewGRPCManagerTunnelConn creates a manager-side gRPC tunnel wrapper.
func NewGRPCManagerTunnelConn(stream grpcManagerStream) *GRPCManagerTunnelConn {
	if stream == nil {
		return &GRPCManagerTunnelConn{}
	}

	recvCtx, cancel := context.WithCancel(stream.Context())
	return &GRPCManagerTunnelConn{
		stream: &cancelableGRPCManagerStream{
			stream: stream,
			ctx:    recvCtx,
		},
		cancel: cancel,
	}
}

// Send sends a manager->agent tunnel message over gRPC.
func (t *GRPCManagerTunnelConn) Send(msg *TunnelMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.IsClosed() {
		return io.EOF
	}

	protoMsg, err := tunnelMessageToManagerProto(msg)
	if err != nil {
		return err
	}

	if err := t.stream.Send(protoMsg); err != nil {
		t.markClosed()
		return err
	}
	return nil
}

// Receive receives an agent->manager tunnel message from gRPC.
func (t *GRPCManagerTunnelConn) Receive() (*TunnelMessage, error) {
	protoMsg, err := t.stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			t.markClosed()
		}
		return nil, err
	}

	msg, err := agentProtoToTunnelMessage(protoMsg)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// IsExpectedReceiveError returns true for expected gRPC stream shutdown errors.
func (t *GRPCManagerTunnelConn) IsExpectedReceiveError(err error) bool {
	return isExpectedGRPCReceiveErrorInternal(err)
}

// Close marks the stream closed on manager side.
func (t *GRPCManagerTunnelConn) Close() error {
	t.markClosed()
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

// IsClosed returns whether the stream is closed.
func (t *GRPCManagerTunnelConn) IsClosed() bool {
	t.closedMu.RLock()
	defer t.closedMu.RUnlock()
	return t.closed
}

// SendRequest sends a request and waits for response.
func (t *GRPCManagerTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return sendRequestWithPending(ctx, t, msg, pending)
}

func (t *GRPCManagerTunnelConn) markClosed() {
	t.closedMu.Lock()
	t.closed = true
	t.closedMu.Unlock()
}

// GRPCAgentTunnelConn wraps the agent-side gRPC tunnel stream.
type GRPCAgentTunnelConn struct {
	stream   grpcAgentStream
	mu       sync.Mutex
	closed   bool
	closedMu sync.RWMutex
}

// NewGRPCAgentTunnelConn creates an agent-side gRPC tunnel wrapper.
func NewGRPCAgentTunnelConn(stream grpcAgentStream) *GRPCAgentTunnelConn {
	return &GRPCAgentTunnelConn{stream: stream}
}

// Send sends an agent->manager tunnel message over gRPC.
func (t *GRPCAgentTunnelConn) Send(msg *TunnelMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.IsClosed() {
		return io.EOF
	}

	protoMsg, err := tunnelMessageToAgentProto(msg)
	if err != nil {
		return err
	}

	if err := t.stream.Send(protoMsg); err != nil {
		t.markClosed()
		return err
	}
	return nil
}

// Receive receives a manager->agent tunnel message from gRPC.
func (t *GRPCAgentTunnelConn) Receive() (*TunnelMessage, error) {
	protoMsg, err := t.stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			t.markClosed()
		}
		return nil, err
	}

	msg, err := managerProtoToTunnelMessage(protoMsg)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// IsExpectedReceiveError returns true for expected gRPC stream shutdown errors.
func (t *GRPCAgentTunnelConn) IsExpectedReceiveError(err error) bool {
	return isExpectedGRPCReceiveErrorInternal(err)
}

// Close closes the client send stream.
func (t *GRPCAgentTunnelConn) Close() error {
	t.markClosed()
	return t.stream.CloseSend()
}

// IsClosed returns whether the stream is closed.
func (t *GRPCAgentTunnelConn) IsClosed() bool {
	t.closedMu.RLock()
	defer t.closedMu.RUnlock()
	return t.closed
}

// SendRequest sends a request and waits for response.
func (t *GRPCAgentTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return sendRequestWithPending(ctx, t, msg, pending)
}

func (t *GRPCAgentTunnelConn) markClosed() {
	t.closedMu.Lock()
	t.closed = true
	t.closedMu.Unlock()
}

func sendRequestWithPending(ctx context.Context, conn interface{ Send(*TunnelMessage) error }, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	ctx, cancel := ensureSendRequestContextInternal(ctx)
	defer cancel()

	respCh := make(chan *TunnelMessage, 1)
	pending.Store(msg.ID, &PendingRequest{
		ResponseCh: respCh,
		CreatedAt:  time.Now(),
	})
	defer pending.Delete(msg.ID)

	if err := conn.Send(msg); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

type cancelableGRPCManagerStream struct {
	stream grpcManagerStream
	ctx    context.Context
}

func (s *cancelableGRPCManagerStream) Send(msg *tunnelpb.ManagerMessage) error {
	return s.stream.Send(msg)
}

func (s *cancelableGRPCManagerStream) Recv() (*tunnelpb.AgentMessage, error) {
	type recvResult struct {
		msg *tunnelpb.AgentMessage
		err error
	}

	recvCh := make(chan recvResult, 1)
	go func() {
		msg, err := s.stream.Recv()
		recvCh <- recvResult{msg: msg, err: err}
	}()

	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case result := <-recvCh:
		return result.msg, result.err
	}
}

func (s *cancelableGRPCManagerStream) Context() context.Context {
	return s.ctx
}

func ensureSendRequestContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), defaultSendRequestTimeout)
	}

	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, defaultSendRequestTimeout)
}

func isExpectedGRPCReceiveErrorInternal(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	code := status.Code(err)
	return code == codes.Canceled || code == codes.DeadlineExceeded
}
