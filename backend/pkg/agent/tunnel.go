package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"

	wshub "getarcane.dev/arcane/v3/pkg/ws"
	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	TunnelMessageType string

	TunnelConnection interface {
		Send(msg *TunnelMessage) error
		Receive() (*TunnelMessage, error)
		Close() error
		IsClosed() bool
		Transport() string
		muxConn(ctx context.Context) *muxConn
	}

	grpcManagerStream interface {
		Send(msg *tunnelpb.ManagerMessage) error
		Recv() (*tunnelpb.AgentMessage, error)
		Context() context.Context
	}

	muxDataCodec struct {
		encoding.CodecV2
	}

	grpcAgentStream interface {
		Send(msg *tunnelpb.AgentMessage) error
		Recv() (*tunnelpb.ManagerMessage, error)
		Context() context.Context
		CloseSend() error
	}
)

const (
	maxGRPCTunnelMessageSize                        = 16 * 1024 * 1024
	maxWebSocketTunnelMessageSize                   = 2 * maxGRPCTunnelMessageSize
	muxPooledFrameMinSize                           = 32 << 10
	muxDataPayloadField                             = protowire.Number(15)
	muxDataBytesField                               = protowire.Number(1)
	MessageTypeRegister           TunnelMessageType = "register"
	MessageTypeRegisterResponse   TunnelMessageType = "register_response"
)

var (
	ErrTunnelConnectionClosed = errors.New("agent tunnel connection is closed")
	tunnelReadWait            = 3 * DefaultHeartbeatInterval

	tunnelCodec  = muxDataCodec{CodecV2: encoding.GetCodecV2(proto.Name)}
	muxFramePool = sync.Pool{New: func() any {
		return new(make([]byte, muxMaxFrameSize))
	}}
)

func NewWebSocketTunnelConn(conn *websocket.Conn) *WebSocketTunnelConn {
	conn.SetReadLimit(maxWebSocketTunnelMessageSize)
	return &WebSocketTunnelConn{conn: conn}
}

func (t *WebSocketTunnelConn) Send(msg *TunnelMessage) error {
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

func (t *WebSocketTunnelConn) Receive() (*TunnelMessage, error) {
	var data bytes.Buffer
	if _, err := t.read(context.Background(), &data); err != nil { //nolint:forbidigo // Tunnel connection owns timed I/O independent of individual HTTP requests.
		return nil, err
	}
	var msg TunnelMessage
	if unmarshalErr := json.Unmarshal(data.Bytes(), &msg); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	return &msg, nil
}

func (t *WebSocketTunnelConn) muxConn(ctx context.Context) *muxConn {
	t.muxed.Store(true)
	var frame bytes.Buffer
	recv := func() ([]byte, error) {
		for {
			messageType, err := t.read(ctx, &frame)
			if err != nil || messageType == websocket.MessageBinary {
				return frame.Bytes(), err
			}
		}
	}
	send := func(p []byte) error {
		return t.write(ctx, websocket.MessageBinary, p, muxWriteTimeout)
	}
	return newMuxConn(send, recv, t.Close)
}

func (t *WebSocketTunnelConn) write(ctx context.Context, messageType websocket.MessageType, data []byte, timeout time.Duration) error {
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

func (t *WebSocketTunnelConn) read(ctx context.Context, buf *bytes.Buffer) (websocket.MessageType, error) {
	if t.closed.Load() {
		return 0, ErrTunnelConnectionClosed
	}
	rctx, cancel := context.WithTimeout(ctx, tunnelReadWait)
	defer cancel()
	buf.Reset()
	messageType, r, err := t.conn.Reader(rctx)
	if err == nil {
		_, err = buf.ReadFrom(r)
	}
	if err != nil {
		t.closed.Store(true)
	}
	return messageType, err
}

func (t *WebSocketTunnelConn) Close() error {
	if t.closed.Swap(true) {
		return t.conn.CloseNow()
	}

	if err := t.conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		return t.conn.CloseNow()
	}
	return nil
}

func (t *WebSocketTunnelConn) IsClosed() bool {
	return t.closed.Load()
}

func (t *WebSocketTunnelConn) Transport() string {
	return TransportWebSocket
}

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

func (t *GRPCManagerTunnelConn) muxConn(context.Context) *muxConn {
	t.muxed.Store(true)
	send := func(p []byte) error {
		return t.send(tunnelpb.ManagerMessage_builder{
			MuxData: tunnelpb.MuxData_builder{Data: p}.Build(),
		}.Build())
	}
	var frame []byte
	recv := func() ([]byte, error) {
		if t.stream == nil {
			return nil, ErrTunnelConnectionClosed
		}
		return receiveMuxData(t.stream.Recv, &t.closed, &frame)
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

func (t *GRPCManagerTunnelConn) Receive() (*TunnelMessage, error) {
	if t.stream == nil {
		return nil, ErrTunnelConnectionClosed
	}
	return receiveGRPC(t.stream.Context(), t.stream.Recv, agentProtoToTunnelMessage, &t.closed)
}

func (t *GRPCManagerTunnelConn) Close() error {
	t.closed.Store(true)
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

func (t *GRPCManagerTunnelConn) IsClosed() bool {
	return t.closed.Load()
}

func (t *GRPCManagerTunnelConn) Transport() string {
	return TransportGRPC
}

func NewGRPCAgentTunnelConn(stream grpcAgentStream, cancel context.CancelFunc) *GRPCAgentTunnelConn {
	return &GRPCAgentTunnelConn{
		stream: stream,
		cancel: cancel,
	}
}

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

func (t *GRPCAgentTunnelConn) muxConn(context.Context) *muxConn {
	t.muxed.Store(true)
	send := func(p []byte) error {
		return t.send(tunnelpb.AgentMessage_builder{
			MuxData: tunnelpb.MuxData_builder{Data: p}.Build(),
		}.Build())
	}
	var frame []byte
	recv := func() ([]byte, error) {
		return receiveMuxData(t.stream.Recv, &t.closed, &frame)
	}
	return newMuxConn(send, recv, t.Close)
}

func (t *GRPCAgentTunnelConn) send(msg *tunnelpb.AgentMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.IsClosed() {
		return ErrTunnelConnectionClosed
	}
	if err := t.stream.Send(msg); err != nil {
		t.closed.Store(true)
		return err
	}
	return nil
}

func (t *GRPCAgentTunnelConn) Receive() (*TunnelMessage, error) {
	return receiveGRPC(t.stream.Context(), t.stream.Recv, managerProtoToTunnelMessage, &t.closed)
}

func (t *GRPCAgentTunnelConn) Close() error {
	if t.closed.Swap(true) {
		return nil
	}

	locked := t.mu.TryLock()
	if !locked {
		t.cancel()
		t.mu.Lock()
	}
	defer t.mu.Unlock()

	err := t.stream.CloseSend()
	if locked {
		t.cancel()
	}
	return err
}

func (t *GRPCAgentTunnelConn) IsClosed() bool {
	return t.closed.Load()
}

func (t *GRPCAgentTunnelConn) Transport() string {
	return TransportGRPC
}

func (s *cancelableGRPCManagerStream) Send(msg *tunnelpb.ManagerMessage) error {
	return s.stream.Send(msg)
}

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

// A gRPC stream is dead after any Recv error.
func receiveMuxData[P interface{ GetMuxData() *tunnelpb.MuxData }](recv func() (P, error), closed *atomic.Bool, frame *[]byte) ([]byte, error) {
	if cap(*frame) == muxMaxFrameSize {
		muxFramePool.Put(new((*frame)[:0]))
	}
	*frame = nil
	for {
		msg, err := recv()
		if err != nil {
			closed.Store(true)
			return nil, err
		}
		if data := msg.GetMuxData(); data != nil {
			*frame = data.GetData()
			return *frame, nil
		}
	}
}

func (c muxDataCodec) Unmarshal(data mem.BufferSlice, v any) error {
	msg, ok := v.(interface{ SetMuxData(v *tunnelpb.MuxData) })
	if !ok {
		return c.CodecV2.Unmarshal(data, v)
	}
	r := data.Reader()
	defer func() {
		_ = r.Close()
	}()
	outerTag, outerErr := binary.ReadUvarint(r)
	outerLen, outerLenErr := binary.ReadUvarint(r)
	if outerErr != nil || outerLenErr != nil || outerTag != protowire.EncodeTag(muxDataPayloadField, protowire.BytesType) || outerLen != uint64(r.Remaining()) {
		return c.CodecV2.Unmarshal(data, v)
	}
	innerTag, innerErr := binary.ReadUvarint(r)
	innerLen, innerLenErr := binary.ReadUvarint(r)
	if innerErr != nil || innerLenErr != nil || innerTag != protowire.EncodeTag(muxDataBytesField, protowire.BytesType) ||
		innerLen != uint64(r.Remaining()) || innerLen < muxPooledFrameMinSize || innerLen > muxMaxFrameSize {
		return c.CodecV2.Unmarshal(data, v)
	}
	payload := (*muxFramePool.Get().(*[]byte))[:innerLen]
	if _, err := io.ReadFull(r, payload); err != nil {
		return err
	}
	msg.SetMuxData(tunnelpb.MuxData_builder{Data: payload}.Build())
	return nil
}

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
