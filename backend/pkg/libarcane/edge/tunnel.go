package edge

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"

	"github.com/coder/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

// TunnelMessageType represents the type of message sent over the tunnel.
type TunnelMessageType string

const (
	maxGRPCTunnelMessageSize = 16 * 1024 * 1024

	// maxWebSocketTunnelMessageSize caps inbound websocket tunnel frames at 2x the
	// gRPC limit, since JSON base64-encodes binary bodies a gRPC peer would accept.
	maxWebSocketTunnelMessageSize = 2 * maxGRPCTunnelMessageSize

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
	// MessageTypeCommandCredit returns flow-control credit for consumed command output.
	MessageTypeCommandCredit TunnelMessageType = "command_credit"
)

var (
	// ErrTunnelConnectionClosed is returned by Send and Receive on every transport once the connection is closed.
	ErrTunnelConnectionClosed = errors.New("edge tunnel connection is closed")
	// tunnelReadWait bounds an idle websocket read; heartbeats arrive every DefaultHeartbeatInterval.
	// It is a variable so tests can shorten it.
	tunnelReadWait = 3 * DefaultHeartbeatInterval
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
	wctx, cancel := context.WithTimeout(context.Background(), DefaultWriteTimeout) //nolint:forbidigo // Tunnel connection owns timed I/O independent of individual HTTP requests.
	defer cancel()
	if writeErr := t.conn.Write(wctx, websocket.MessageText, data); writeErr != nil {
		// coder/websocket closes the connection after a failed or expired write.
		t.closed.Store(true)
		return writeErr
	}
	return nil
}

// Receive receives a tunnel message from the WebSocket connection.
func (t *TunnelConn) Receive() (*TunnelMessage, error) {
	if t.closed.Load() {
		return nil, ErrTunnelConnectionClosed
	}

	rctx, cancel := context.WithTimeout(context.Background(), tunnelReadWait) //nolint:forbidigo // Tunnel connection owns timed I/O independent of individual HTTP requests.
	defer cancel()
	_, data, err := t.conn.Read(rctx)
	if err != nil {
		// Read-timeout expiry and network errors are terminal: coder/websocket
		// closes the connection when a read context expires.
		t.closed.Store(true)
		return nil, err
	}

	var msg TunnelMessage
	if unmarshalErr := json.Unmarshal(data, &msg); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	return &msg, nil
}

// IsExpectedReceiveError returns true for normal WebSocket close/teardown errors.
func (t *TunnelConn) IsExpectedReceiveError(err error) bool {
	return isExpectedReceiveError(err)
}

// Close closes the WebSocket tunnel connection, sending a close frame on the
// first call so the peer observes a normal closure instead of 1006.
func (t *TunnelConn) Close() error {
	if t.closed.Swap(true) {
		return t.conn.CloseNow()
	}

	// Close performs the close handshake with its own internal timeout and is
	// safe concurrently with a data writer, so a stuck Send cannot block teardown.
	if err := t.conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		return t.conn.CloseNow()
	}
	return nil
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
			recvCh: make(chan grpcRecvResult),
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

	protoMsg, err := tunnelMessageToManagerProto(msg, t.parity)
	if err != nil {
		return err
	}

	if sendErr := t.stream.Send(protoMsg); sendErr != nil {
		t.closed.Store(true)
		return sendErr
	}
	return nil
}

// Receive receives an agent->manager tunnel message from gRPC.
func (t *GRPCManagerTunnelConn) Receive() (*TunnelMessage, error) {
	if t.stream == nil {
		return nil, ErrTunnelConnectionClosed
	}
	return receiveGRPC(t.stream.Context(), t.stream.Recv, agentProtoToTunnelMessage, &t.closed)
}

// IsExpectedReceiveError returns true for expected gRPC stream shutdown errors.
func (t *GRPCManagerTunnelConn) IsExpectedReceiveError(err error) bool {
	return isExpectedReceiveError(err)
}

// Close marks the stream closed on manager side.
func (t *GRPCManagerTunnelConn) Close() error {
	t.closed.Store(true)
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

// IsClosed returns whether the stream is closed.
func (t *GRPCManagerTunnelConn) IsClosed() bool {
	return t.closed.Load()
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

	if sendErr := t.stream.Send(protoMsg); sendErr != nil {
		t.closed.Store(true)
		return sendErr
	}
	return nil
}

// Receive receives a manager->agent tunnel message from gRPC.
func (t *GRPCAgentTunnelConn) Receive() (*TunnelMessage, error) {
	if t.stream == nil {
		return nil, ErrTunnelConnectionClosed
	}
	return receiveGRPC(t.stream.Context(), t.stream.Recv, managerProtoToTunnelMessage, &t.closed)
}

// IsExpectedReceiveError returns true for expected gRPC stream shutdown errors.
func (t *GRPCAgentTunnelConn) IsExpectedReceiveError(err error) bool {
	return isExpectedReceiveError(err)
}

// Close half-closes the send stream before cancelling, so the manager sees a
// clean io.EOF instead of codes.Canceled.
func (t *GRPCAgentTunnelConn) Close() error {
	if t.closed.Swap(true) {
		return nil
	}

	// Preserve a clean half-close unless an in-flight send must be cancelled first.
	locked := t.mu.TryLock()
	if !locked {
		if t.cancel != nil {
			t.cancel()
		}
		t.mu.Lock()
	}
	defer t.mu.Unlock()

	var err error
	if t.stream != nil {
		err = t.stream.CloseSend()
	}
	if locked && t.cancel != nil {
		t.cancel()
	}
	return err
}

// IsClosed returns whether the stream is closed.
func (t *GRPCAgentTunnelConn) IsClosed() bool {
	return t.closed.Load()
}

// Transport identifies the underlying tunnel transport.
func (t *GRPCAgentTunnelConn) Transport() string {
	return EdgeTransportGRPC
}

func (s *cancelableGRPCManagerStream) Send(msg *tunnelpb.ManagerMessage) error {
	return s.stream.Send(msg)
}

// Recv returns the next message, or ctx's error once ctx is done. gRPC Recv ignores ctx,
// so a single reader goroutine waits on the stream instead.
func (s *cancelableGRPCManagerStream) Recv() (*tunnelpb.AgentMessage, error) {
	s.recvOnce.Do(func() {
		go func() {
			for {
				msg, err := s.stream.Recv()
				select {
				case s.recvCh <- grpcRecvResult{msg: msg, err: err}:
				case <-s.ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	})

	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case result := <-s.recvCh:
		return result.msg, result.err
	}
}

func (s *cancelableGRPCManagerStream) Context() context.Context {
	return s.ctx
}

// receiveGRPC decodes the next stream message, skipping payloads from newer peers
// that this build cannot decode, as unknown websocket message types are skipped.
func receiveGRPC[P any](ctx context.Context, recv func() (P, error), decode func(P) (*TunnelMessage, error), closed *atomic.Bool) (*TunnelMessage, error) {
	for {
		protoMsg, err := recv()
		if err != nil {
			// A gRPC stream is dead after any Recv error, not just io.EOF.
			closed.Store(true)
			return nil, err
		}
		msg, err := decode(protoMsg)
		if errors.Is(err, errUnknownTunnelPayload) {
			slog.DebugContext(ctx, "Ignoring unknown edge tunnel payload", "error", err)
			continue
		}
		return msg, err
	}
}

// isExpectedReceiveError reports normal teardown on either transport: EOF, cancellation,
// a closed connection, a clean websocket close, or a cancelled gRPC stream.
func isExpectedReceiveError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, ErrTunnelConnectionClosed) || wshub.IsExpectedClose(err) {
		return true
	}
	code := status.Code(err)
	return code == codes.Canceled || code == codes.DeadlineExceeded
}
