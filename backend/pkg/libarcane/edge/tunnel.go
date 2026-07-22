package edge

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TunnelMessageType represents the type of message sent over the tunnel.
type TunnelMessageType string

const maxGRPCTunnelMessageSize = 16 * 1024 * 1024

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
	// MessageTypeCommandRequest sends a typed edge command from manager to agent.
	MessageTypeCommandRequest TunnelMessageType = "command_request"
	// MessageTypeCommandAck acknowledges a command was accepted by the agent.
	MessageTypeCommandAck TunnelMessageType = "command_ack"
	// MessageTypeCommandOutput carries chunked command output from agent to manager.
	MessageTypeCommandOutput TunnelMessageType = "command_output"
	// MessageTypeCommandComplete indicates final command completion.
	MessageTypeCommandComplete TunnelMessageType = "command_complete"
	// MessageTypeFileChunk carries chunked request or response payload data.
	MessageTypeFileChunk TunnelMessageType = "file_chunk"
	// MessageTypeStreamOpen opens a command-backed stream.
	MessageTypeStreamOpen TunnelMessageType = "stream_open"
	// MessageTypeStreamClose closes a command-backed stream.
	MessageTypeStreamClose TunnelMessageType = "stream_close"
	// MessageTypeCancelRequest requests cancellation of an in-flight command.
	MessageTypeCancelRequest TunnelMessageType = "cancel_request"
)

// TunnelConnection is the transport contract shared by WebSocket and gRPC wrappers.
type TunnelConnection interface {
	Send(msg *TunnelMessage) error
	Receive() (*TunnelMessage, error)
	IsExpectedReceiveError(err error) bool
	Close() error
	IsClosed() bool
}

// NewTunnelConn creates a new WebSocket tunnel connection wrapper.
func NewTunnelConn(conn *websocket.Conn) *TunnelConn {
	return &TunnelConn{conn: conn}
}

// Send sends a tunnel message over the WebSocket connection.
func (t *TunnelConn) Send(msg *TunnelMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed.Load() {
		return websocket.ErrCloseSent
	}

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
	t.closed.Store(true)

	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Close()
}

// IsClosed returns whether the connection is closed.
func (t *TunnelConn) IsClosed() bool {
	return t.closed.Load()
}

type grpcManagerStream interface {
	Send(msg *tunnelpb.ManagerMessage) error
	Recv() (*tunnelpb.AgentMessage, error)
	Context() context.Context
}

type grpcAgentStream interface {
	Send(msg *tunnelpb.AgentMessage) error
	Recv() (*tunnelpb.ManagerMessage, error)
	Context() context.Context
	CloseSend() error
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
	return t.closed.Load()
}

func (t *GRPCManagerTunnelConn) markClosed() {
	t.closed.Store(true)
}

// NewGRPCAgentTunnelConn creates an agent-side gRPC tunnel wrapper.
func NewGRPCAgentTunnelConn(stream grpcAgentStream, cancelFns ...context.CancelFunc) *GRPCAgentTunnelConn {
	var cancel context.CancelFunc
	if len(cancelFns) > 0 {
		cancel = cancelFns[0]
	}

	return &GRPCAgentTunnelConn{stream: stream, cancel: cancel}
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
	if t.cancel != nil {
		t.cancel()
	}
	if t.stream == nil {
		return nil
	}
	return t.stream.CloseSend()
}

// IsClosed returns whether the stream is closed.
func (t *GRPCAgentTunnelConn) IsClosed() bool {
	return t.closed.Load()
}

func (t *GRPCAgentTunnelConn) markClosed() {
	t.closed.Store(true)
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
