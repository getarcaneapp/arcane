package agent

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	wshub "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/ws"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v3/proto/tunnel/v1"
)

// TunnelMessageType represents the type of message sent over the tunnel.
type TunnelMessageType string

const (
	maxGRPCTunnelMessageSize = 16 * 1024 * 1024

	// maxWebSocketTunnelMessageSize caps inbound websocket tunnel frames at 2x the
	// gRPC limit, since JSON base64-encodes binary bodies a gRPC peer would accept.
	maxWebSocketTunnelMessageSize = 2 * maxGRPCTunnelMessageSize

	// MessageTypeRegister is the first message sent by the agent on gRPC transport.
	MessageTypeRegister TunnelMessageType = "register"
	// MessageTypeRegisterResponse is sent by manager after register validation.
	MessageTypeRegisterResponse TunnelMessageType = "register_response"
)

var (
	// ErrTunnelConnectionClosed is returned by Send and Receive on every transport once the connection is closed.
	ErrTunnelConnectionClosed = errors.New("agent tunnel connection is closed")
	// tunnelReadWait bounds an idle websocket read; heartbeats arrive every DefaultHeartbeatInterval.
	// It is a variable so tests can shorten it.
	tunnelReadWait = 3 * DefaultHeartbeatInterval
)

// TunnelConnection is the transport contract shared by WebSocket and gRPC wrappers.
type TunnelConnection interface {
	Send(msg *TunnelMessage) error
	Receive() (*TunnelMessage, error)
	Close() error
	IsClosed() bool
	Transport() string
	muxConn(ctx context.Context) io.ReadWriteCloser
}

// NewTunnelConn creates a new WebSocket tunnel connection wrapper.
func NewTunnelConn(conn *websocket.Conn) *TunnelConn {
	conn.SetReadLimit(maxWebSocketTunnelMessageSize)
	return &TunnelConn{conn: conn}
}

// Send sends a register message over the WebSocket connection.
func (t *TunnelConn) Send(msg *TunnelMessage) error {
	if t.muxed.Load() {
		return errTunnelMuxActive
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	//nolint:forbidigo // Tunnel connection owns timed I/O independent of individual HTTP requests.
	return t.write(context.Background(), websocket.MessageText, data, DefaultWriteTimeout)
}

// Receive receives a register message from the WebSocket connection.
func (t *TunnelConn) Receive() (*TunnelMessage, error) {
	_, data, err := t.read(context.Background()) //nolint:forbidigo // Tunnel connection owns timed I/O independent of individual HTTP requests.
	if err != nil {
		return nil, err
	}
	var msg TunnelMessage
	if unmarshalErr := json.Unmarshal(data, &msg); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	return &msg, nil
}

// muxConn switches the connection to binary frames carrying the multiplexed session.
func (t *TunnelConn) muxConn(ctx context.Context) io.ReadWriteCloser {
	t.muxed.Store(true)
	recv := func() ([]byte, error) {
		for {
			messageType, data, err := t.read(ctx)
			if err != nil || messageType == websocket.MessageBinary {
				return data, err
			}
		}
	}
	send := func(p []byte) error { return t.write(ctx, websocket.MessageBinary, p, muxWriteTimeout) }
	return newMuxConn(send, recv, t.Close)
}

// write sends one frame. coder/websocket closes the connection after a failed or expired write.
func (t *TunnelConn) write(ctx context.Context, messageType websocket.MessageType, data []byte, timeout time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed.Load() {
		return ErrTunnelConnectionClosed
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := t.conn.Write(wctx, messageType, data); err != nil {
		t.closed.Store(true)
		return err
	}
	return nil
}

// read receives one frame. Read-timeout expiry and network errors are terminal: coder/websocket
// closes the connection when a read context expires.
func (t *TunnelConn) read(ctx context.Context) (websocket.MessageType, []byte, error) {
	if t.closed.Load() {
		return 0, nil, ErrTunnelConnectionClosed
	}
	rctx, cancel := context.WithTimeout(ctx, tunnelReadWait)
	defer cancel()
	messageType, data, err := t.conn.Read(rctx)
	if err != nil {
		t.closed.Store(true)
	}
	return messageType, data, err
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
	return TransportWebSocket
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

// NewGRPCManagerTunnelConn creates a manager-side gRPC tunnel wrapper. A nil stream yields a connection
// whose Send and Receive report ErrTunnelConnectionClosed.
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

// Send sends a manager->agent register message over gRPC.
func (t *GRPCManagerTunnelConn) Send(msg *TunnelMessage) error {
	if t.muxed.Load() {
		return errTunnelMuxActive
	}
	protoMsg, err := tunnelMessageToManagerProto(msg)
	if err != nil {
		return err
	}
	return t.send(protoMsg)
}

// muxConn switches the stream to MuxData messages carrying the multiplexed session.
func (t *GRPCManagerTunnelConn) muxConn(context.Context) io.ReadWriteCloser {
	t.muxed.Store(true)
	send := func(p []byte) error {
		return t.send(&tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_MuxData{MuxData: &tunnelpb.MuxData{Data: p}}})
	}
	recv := func() ([]byte, error) {
		if t.stream == nil {
			return nil, ErrTunnelConnectionClosed
		}
		return receiveMuxData(t.stream.Recv, &t.closed)
	}
	return newMuxConn(send, recv, t.Close)
}

func (t *GRPCManagerTunnelConn) send(msg *tunnelpb.ManagerMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.IsClosed() || t.stream == nil {
		return ErrTunnelConnectionClosed
	}
	if err := t.stream.Send(msg); err != nil {
		t.closed.Store(true)
		return err
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
	return TransportGRPC
}

// NewGRPCAgentTunnelConn creates an agent-side gRPC tunnel wrapper.
func NewGRPCAgentTunnelConn(stream grpcAgentStream, cancelFns ...context.CancelFunc) *GRPCAgentTunnelConn {
	var cancel context.CancelFunc
	if len(cancelFns) > 0 {
		cancel = cancelFns[0]
	}

	return &GRPCAgentTunnelConn{stream: stream, cancel: cancel}
}

// Send sends an agent->manager register message over gRPC.
func (t *GRPCAgentTunnelConn) Send(msg *TunnelMessage) error {
	if t.muxed.Load() {
		return errTunnelMuxActive
	}
	protoMsg, err := tunnelMessageToAgentProto(msg)
	if err != nil {
		return err
	}
	return t.send(protoMsg)
}

// muxConn switches the stream to MuxData messages carrying the multiplexed session.
func (t *GRPCAgentTunnelConn) muxConn(context.Context) io.ReadWriteCloser {
	t.muxed.Store(true)
	send := func(p []byte) error {
		return t.send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_MuxData{MuxData: &tunnelpb.MuxData{Data: p}}})
	}
	recv := func() ([]byte, error) {
		if t.stream == nil {
			return nil, ErrTunnelConnectionClosed
		}
		return receiveMuxData(t.stream.Recv, &t.closed)
	}
	return newMuxConn(send, recv, t.Close)
}

func (t *GRPCAgentTunnelConn) send(msg *tunnelpb.AgentMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.IsClosed() || t.stream == nil {
		return ErrTunnelConnectionClosed
	}
	if err := t.stream.Send(msg); err != nil {
		t.closed.Store(true)
		return err
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
	return TransportGRPC
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
			slog.DebugContext(ctx, "Ignoring unknown agent tunnel payload", "error", err)
			continue
		}
		return msg, err
	}
}

// receiveMuxData returns the next MuxData payload, skipping any other payload still in flight.
// A gRPC stream is dead after any Recv error.
func receiveMuxData[P interface{ GetMuxData() *tunnelpb.MuxData }](recv func() (P, error), closed *atomic.Bool) ([]byte, error) {
	for {
		msg, err := recv()
		if err != nil {
			closed.Store(true)
			return nil, err
		}
		if data := msg.GetMuxData(); data != nil {
			return data.GetData(), nil
		}
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
