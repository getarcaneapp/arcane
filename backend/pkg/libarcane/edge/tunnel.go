package edge

import (
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net"
	"time"

	"emperror.dev/errors"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TunnelMessageType represents the type of message sent over the tunnel.
type TunnelMessageType string

const maxGRPCTunnelMessageSize = 16 * 1024 * 1024

// maxWebSocketTunnelMessageSize caps inbound websocket tunnel frames at 2x the
// gRPC limit, since JSON base64-encodes binary bodies a gRPC peer would accept.
const maxWebSocketTunnelMessageSize = 2 * maxGRPCTunnelMessageSize

// ErrTunnelConnectionClosed is returned by Send and Receive on every transport
// once the tunnel connection is closed.
const ErrTunnelConnectionClosed = errors.Sentinel("edge tunnel connection is closed")

// tunnelReadWait bounds how long a websocket tunnel read may sit idle. Both
// peers see traffic at least every DefaultHeartbeatInterval (agent heartbeat,
// manager heartbeat_ack), so 3x tolerates transient stalls while still
// detecting a silently dead peer. Variable so tests can shorten it.
var tunnelReadWait = 3 * DefaultHeartbeatInterval

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
	Transport() string
}

// NewTunnelConn creates a new WebSocket tunnel connection wrapper.
func NewTunnelConn(conn *websocket.Conn) *TunnelConn {
	conn.SetReadLimit(maxWebSocketTunnelMessageSize)
	return &TunnelConn{conn: conn}
}

// Send sends a tunnel message over the WebSocket connection.
func (t *TunnelConn) Send(msg *TunnelMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed.Load() {
		return ErrTunnelConnectionClosed
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_ = t.conn.SetWriteDeadline(time.Now().Add(DefaultWriteTimeout))
	if err := t.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		// A gorilla connection is unusable after a write error.
		t.closed.Store(true)
		return err
	}
	return nil
}

// Receive receives a tunnel message from the WebSocket connection.
func (t *TunnelConn) Receive() (*TunnelMessage, error) {
	if t.closed.Load() {
		return nil, ErrTunnelConnectionClosed
	}

	_ = t.conn.SetReadDeadline(time.Now().Add(tunnelReadWait))
	_, data, err := t.conn.ReadMessage()
	if err != nil {
		// Read-deadline expiry and network errors are terminal for gorilla conns.
		t.closed.Store(true)
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
	if errors.Is(err, net.ErrClosed) || errors.Is(err, ErrTunnelConnectionClosed) {
		return true
	}

	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

// Close closes the WebSocket tunnel connection, sending a close frame on the
// first call so the peer observes a normal closure instead of 1006.
func (t *TunnelConn) Close() error {
	if t.closed.Swap(true) {
		return t.conn.Close()
	}

	// WriteControl is safe concurrently with a data writer, so a stuck Send
	// cannot block teardown.
	_ = t.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(DefaultWriteTimeout))
	return t.conn.Close()
}

// IsClosed returns whether the connection is closed.
func (t *TunnelConn) IsClosed() bool {
	return t.closed.Load()
}

// Transport identifies the underlying tunnel transport.
func (t *TunnelConn) Transport() string {
	return EdgeTransportWebSocket
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

// NewGRPCManagerTunnelConn creates a manager-side gRPC tunnel wrapper. A nil
// stream yields a connection whose Send/Receive report the closed sentinel
// instead of dereferencing nil.
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

	if t.IsClosed() || t.stream == nil {
		return ErrTunnelConnectionClosed
	}

	protoMsg, err := tunnelMessageToManagerProto(msg, t.parityEncoding.Load())
	if err != nil {
		return err
	}

	if err := t.stream.Send(protoMsg); err != nil {
		t.markClosed()
		return err
	}
	return nil
}

// SetParityEncoding enables native proto encodings for message types that are
// otherwise re-encoded for legacy agents. Set when the agent advertises the
// proto-parity-v1 capability during registration.
func (t *GRPCManagerTunnelConn) SetParityEncoding(enabled bool) {
	t.parityEncoding.Store(enabled)
}

// Receive receives an agent->manager tunnel message from gRPC.
func (t *GRPCManagerTunnelConn) Receive() (*TunnelMessage, error) {
	if t.stream == nil {
		return nil, ErrTunnelConnectionClosed
	}

	for {
		protoMsg, err := t.stream.Recv()
		if err != nil {
			// A gRPC stream is dead after any Recv error, not just io.EOF.
			t.markClosed()
			return nil, err
		}

		msg, err := agentProtoToTunnelMessage(protoMsg)
		if err != nil {
			if errors.Is(err, errUnknownTunnelPayload) {
				// A newer peer sent a payload this build cannot decode yet;
				// skip it like unknown websocket message types are skipped.
				slog.Debug("Ignoring unknown edge tunnel payload", "error", err)
				continue
			}
			return nil, err
		}
		return msg, nil
	}
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

// Transport identifies the underlying tunnel transport.
func (t *GRPCManagerTunnelConn) Transport() string {
	return EdgeTransportGRPC
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

	if t.IsClosed() || t.stream == nil {
		return ErrTunnelConnectionClosed
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
	if t.stream == nil {
		return nil, ErrTunnelConnectionClosed
	}

	for {
		protoMsg, err := t.stream.Recv()
		if err != nil {
			// A gRPC stream is dead after any Recv error, not just io.EOF.
			t.markClosed()
			return nil, err
		}

		msg, err := managerProtoToTunnelMessage(protoMsg)
		if err != nil {
			if errors.Is(err, errUnknownTunnelPayload) {
				// A newer peer sent a payload this build cannot decode yet;
				// skip it like unknown websocket message types are skipped.
				slog.Debug("Ignoring unknown edge tunnel payload", "error", err)
				continue
			}
			return nil, err
		}
		return msg, nil
	}
}

// IsExpectedReceiveError returns true for expected gRPC stream shutdown errors.
func (t *GRPCAgentTunnelConn) IsExpectedReceiveError(err error) bool {
	return isExpectedGRPCReceiveErrorInternal(err)
}

// Close closes the client send stream. The half-close happens before cancel so
// the manager observes a clean io.EOF (the gRPC analog of a websocket close
// frame) instead of codes.Canceled.
func (t *GRPCAgentTunnelConn) Close() error {
	if t.closed.Swap(true) {
		return nil
	}

	var err error
	if t.stream != nil {
		err = t.stream.CloseSend()
	}
	if t.cancel != nil {
		t.cancel()
	}
	return err
}

// IsClosed returns whether the stream is closed.
func (t *GRPCAgentTunnelConn) IsClosed() bool {
	return t.closed.Load()
}

func (t *GRPCAgentTunnelConn) markClosed() {
	t.closed.Store(true)
}

// Transport identifies the underlying tunnel transport.
func (t *GRPCAgentTunnelConn) Transport() string {
	return EdgeTransportGRPC
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
	if errors.Is(err, ErrTunnelConnectionClosed) {
		return true
	}

	code := status.Code(err)
	return code == codes.Canceled || code == codes.DeadlineExceeded
}
