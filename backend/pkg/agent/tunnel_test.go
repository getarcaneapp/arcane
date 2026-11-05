package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	blockingGRPCManagerStream struct {
		ctx         context.Context
		recvStarted chan struct{}
	}

	closeOrderAgentStream struct {
		ctx    context.Context
		events []string
	}
)

func newWebSocketAgentTunnel(envID string, conn *websocket.Conn) *AgentTunnel {
	return NewAgentTunnel(AgentTunnelConfig{EnvironmentID: envID, Conn: NewWebSocketTunnelConn(conn)})
}

func TestTunnelConn(t *testing.T) {
	// Start a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for {
			mt, message, readErr := conn.Read(r.Context())
			if readErr != nil {
				break
			}
			// Echo back
			readErr = conn.Write(r.Context(), mt, message)
			if readErr != nil {
				break
			}
		}
	}))
	defer server.Close()

	// Connect to the server
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	tunnelConn := NewWebSocketTunnelConn(conn)

	// Test Send and Receive
	msg := &TunnelMessage{
		ID:         "test-id",
		Type:       MessageTypeRegister,
		AgentToken: "test-token",
	}

	err = tunnelConn.Send(msg)
	require.NoError(t, err)

	received, err := tunnelConn.Receive()
	require.NoError(t, err)
	assert.Equal(t, msg.ID, received.ID)
	assert.Equal(t, msg.Type, received.Type)
	assert.Equal(t, msg.AgentToken, received.AgentToken)

	// Test IsClosed
	assert.False(t, tunnelConn.IsClosed())

	err = tunnelConn.Close()
	require.NoError(t, err)
	assert.True(t, tunnelConn.IsClosed())

	// Test Send on closed connection
	err = tunnelConn.Send(msg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTunnelConnectionClosed)
}

func TestGRPCManagerTunnelConn_CloseCancelsReceive(t *testing.T) {
	baseCtx := t.Context()

	stream := &blockingGRPCManagerStream{
		ctx:         baseCtx,
		recvStarted: make(chan struct{}, 1),
	}
	conn := NewGRPCManagerTunnelConn(stream)

	errCh := make(chan error, 1)
	go func() {
		_, err := conn.Receive()
		errCh <- err
	}()

	select {
	case <-stream.recvStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for stream recv start")
	}

	require.NoError(t, conn.Close())

	select {
	case err := <-errCh:
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for receive to unblock")
	}
}

func (b *blockingGRPCManagerStream) Send(*tunnelpb.ManagerMessage) error { return nil }

func (b *blockingGRPCManagerStream) Recv() (*tunnelpb.AgentMessage, error) {
	select {
	case b.recvStarted <- struct{}{}:
	default:
	}
	<-b.ctx.Done()
	return nil, b.ctx.Err()
}

func (b *blockingGRPCManagerStream) Context() context.Context { return b.ctx }

func TestTunnelConn_CloseSendsCloseFrame(t *testing.T) {
	peerRead := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, _, err = conn.Read(r.Context())
		peerRead <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	require.NoError(t, err)

	tunnelConn := NewWebSocketTunnelConn(conn)
	require.NoError(t, tunnelConn.Close())

	select {
	case operationErr := <-peerRead:
		assert.Equal(t, websocket.StatusNormalClosure, websocket.CloseStatus(operationErr),
			"peer should observe a normal closure, got: %v", operationErr)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for peer read to return")
	}
}

func TestTunnelConn_ReceiveDeadlineMarksClosed(t *testing.T) {
	previousReadWait := tunnelReadWait
	tunnelReadWait = 100 * time.Millisecond
	t.Cleanup(func() { tunnelReadWait = previousReadWait })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		// Keep the socket open but never send anything.
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	require.NoError(t, err)

	tunnelConn := NewWebSocketTunnelConn(conn)
	_, err = tunnelConn.Receive()
	require.Error(t, err)
	assert.True(t, tunnelConn.IsClosed())
}

func (s *closeOrderAgentStream) Send(*tunnelpb.AgentMessage) error { return nil }

func (s *closeOrderAgentStream) Recv() (*tunnelpb.ManagerMessage, error) { return nil, io.EOF }

func (s *closeOrderAgentStream) Context() context.Context { return s.ctx }

func (s *closeOrderAgentStream) CloseSend() error {
	s.events = append(s.events, "close_send")
	return nil
}

func TestGRPCAgentTunnelConn_CloseHalfClosesBeforeCancel(t *testing.T) {
	stream := &closeOrderAgentStream{ctx: t.Context()}
	conn := NewGRPCAgentTunnelConn(stream, func() { stream.events = append(stream.events, "cancel") })

	require.NoError(t, conn.Close())
	assert.Equal(t, []string{"close_send", "cancel"}, stream.events)

	// A second close is a no-op.
	require.NoError(t, conn.Close())
	assert.Equal(t, []string{"close_send", "cancel"}, stream.events)
}

func muxPattern(size int, seed byte) []byte {
	pattern := make([]byte, size)
	for i := range pattern {
		pattern[i] = seed + byte(i*7)
	}
	return pattern
}

func wireBytesField(num protowire.Number, parts ...[]byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), bytes.Join(parts, nil))
}

func marshalTunnelMessage(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	raw, err := proto.Marshal(msg)
	require.NoError(t, err)
	return raw
}

func tunnelMessageReader[P interface {
	proto.Message
	GetMuxData() *tunnelpb.MuxData
}](raw [][]byte) func() (P, error) {
	next := 0
	return func() (P, error) {
		var zero P
		if next == len(raw) {
			return zero, io.EOF
		}
		msg := zero.ProtoReflect().New().Interface().(P)
		err := tunnelCodec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(raw[next])}, msg)
		next++
		return msg, err
	}
}

func TestMuxDataCodecUnmarshalPooling(t *testing.T) {
	tests := []struct {
		name   string
		size   int
		pooled bool
	}{
		{
			name: "just under pooled minimum",
			size: muxPooledFrameMinSize - 1,
		},
		{
			name:   "pooled minimum",
			size:   muxPooledFrameMinSize,
			pooled: true,
		},
		{
			name:   "max frame size",
			size:   muxMaxFrameSize,
			pooled: true,
		},
		{
			name: "above max frame size",
			size: muxMaxFrameSize + 1,
		},
	}
	for i, tt := range tests {
		payload := muxPattern(tt.size, byte(i+1))
		for _, msg := range []proto.Message{
			tunnelpb.AgentMessage_builder{MuxData: tunnelpb.MuxData_builder{Data: payload}.Build()}.Build(),
			tunnelpb.ManagerMessage_builder{MuxData: tunnelpb.MuxData_builder{Data: payload}.Build()}.Build(),
		} {
			t.Run(tt.name+"/"+string(msg.ProtoReflect().Descriptor().Name()), func(t *testing.T) {
				got := msg.ProtoReflect().New().Interface()
				require.NoError(t, tunnelCodec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(marshalTunnelMessage(t, msg))}, got))
				data := got.(interface{ GetMuxData() *tunnelpb.MuxData }).GetMuxData().GetData()
				assert.True(t, bytes.Equal(payload, data))
				assert.True(t, proto.Equal(msg, got))
				assert.Equal(t, tt.pooled, cap(data) == muxMaxFrameSize)
			})
		}
	}
}

func TestMuxDataCodecUnmarshalFallback(t *testing.T) {
	payload := muxPattern(muxPooledFrameMinSize, 0x5a)
	tests := []struct {
		target  proto.Message
		name    string
		raw     []byte
		wantErr bool
	}{
		{
			name: "agent register",
			raw: marshalTunnelMessage(t, tunnelpb.AgentMessage_builder{
				Register: tunnelpb.RegisterRequest_builder{
					AgentToken:   "token",
					Capabilities: []string{tunnelCapabilityMux},
				}.Build(),
			}.Build()),
			target: &tunnelpb.AgentMessage{},
		},
		{
			name: "manager register response",
			raw: marshalTunnelMessage(t, tunnelpb.ManagerMessage_builder{
				RegisterResponse: tunnelpb.RegisterResponse_builder{
					Accepted:     true,
					Capabilities: []string{tunnelCapabilityMuxGrant},
				}.Build(),
			}.Build()),
			target: &tunnelpb.ManagerMessage{},
		},
		{
			name:   "empty mux data",
			raw:    wireBytesField(muxDataPayloadField),
			target: &tunnelpb.ManagerMessage{},
		},
		{
			name: "mux data with extra inner field",
			raw: wireBytesField(
				muxDataPayloadField,
				wireBytesField(muxDataBytesField, payload),
				protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), 7),
			),
			target: &tunnelpb.AgentMessage{},
		},
		{
			name: "message with extra outer field",
			raw: bytes.Join([][]byte{
				wireBytesField(muxDataPayloadField, wireBytesField(muxDataBytesField, payload)),
				protowire.AppendVarint(protowire.AppendTag(nil, 20, protowire.VarintType), 7),
			}, nil),
			target: &tunnelpb.ManagerMessage{},
		},
		{
			name: "mux data split across repeated data fields",
			raw: wireBytesField(
				muxDataPayloadField,
				wireBytesField(muxDataBytesField, payload[:len(payload)/2]),
				wireBytesField(muxDataBytesField, payload),
			),
			target: &tunnelpb.AgentMessage{},
		},
		{
			name:    "truncated mux data",
			raw:     wireBytesField(muxDataPayloadField, wireBytesField(muxDataBytesField, payload))[:muxPooledFrameMinSize/2],
			target:  &tunnelpb.ManagerMessage{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.target.ProtoReflect().New().Interface()
			wantErr := proto.Unmarshal(tt.raw, want)
			err := tunnelCodec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(tt.raw)}, tt.target)
			if tt.wantErr {
				require.Error(t, wantErr)
				require.Error(t, err)
				return
			}
			require.NoError(t, wantErr)
			require.NoError(t, err)
			assert.True(t, proto.Equal(want, tt.target))
			assert.NotEqual(t, muxMaxFrameSize, cap(tt.target.(interface{ GetMuxData() *tunnelpb.MuxData }).GetMuxData().GetData()))
		})
	}
}

func TestReceiveMuxDataKeepsFramesUntilRecycled(t *testing.T) {
	sizes := []int{
		muxPooledFrameMinSize,
		muxMaxFrameSize,
		muxPooledFrameMinSize - 1,
		muxMaxFrameSize,
		muxPooledFrameMinSize,
		muxMaxFrameSize,
	}
	var managerRaw, agentRaw [][]byte
	var managerWant, agentWant [][]byte
	for i, size := range sizes {
		managerWant = append(managerWant, muxPattern(size, byte(2*i+1)))
		agentWant = append(agentWant, muxPattern(size, byte(2*i+2)))
		if i%2 == 1 {
			managerRaw = append(managerRaw, marshalTunnelMessage(t, tunnelpb.ManagerMessage_builder{
				RegisterResponse: tunnelpb.RegisterResponse_builder{Accepted: true}.Build(),
			}.Build()))
			agentRaw = append(agentRaw, marshalTunnelMessage(t, tunnelpb.AgentMessage_builder{
				Register: tunnelpb.RegisterRequest_builder{AgentToken: "token"}.Build(),
			}.Build()))
		}
		managerRaw = append(managerRaw, marshalTunnelMessage(t, tunnelpb.ManagerMessage_builder{
			MuxData: tunnelpb.MuxData_builder{Data: managerWant[i]}.Build(),
		}.Build()))
		agentRaw = append(agentRaw, marshalTunnelMessage(t, tunnelpb.AgentMessage_builder{
			MuxData: tunnelpb.MuxData_builder{Data: agentWant[i]}.Build(),
		}.Build()))
	}

	managerRecv := tunnelMessageReader[*tunnelpb.ManagerMessage](managerRaw)
	agentRecv := tunnelMessageReader[*tunnelpb.AgentMessage](agentRaw)
	var managerClosed, agentClosed atomic.Bool
	var managerFrame, agentFrame, previousAgent []byte
	for i, size := range sizes {
		gotManager, err := receiveMuxData(managerRecv, &managerClosed, &managerFrame)
		require.NoError(t, err)
		require.True(t, bytes.Equal(managerWant[i], gotManager), "manager read %d decoded wrong bytes", i)
		if i > 0 {
			require.True(t, bytes.Equal(agentWant[i-1], previousAgent), "agent frame %d overwritten before it was recycled", i-1)
		}

		gotAgent, err := receiveMuxData(agentRecv, &agentClosed, &agentFrame)
		require.NoError(t, err)
		require.True(t, bytes.Equal(agentWant[i], gotAgent), "agent read %d decoded wrong bytes", i)
		require.True(t, bytes.Equal(managerWant[i], gotManager), "manager frame %d overwritten before it was recycled", i)

		assert.Equal(t, size >= muxPooledFrameMinSize, cap(gotManager) == muxMaxFrameSize)
		assert.Equal(t, size >= muxPooledFrameMinSize, cap(gotAgent) == muxMaxFrameSize)
		previousAgent = gotAgent
	}

	_, err := receiveMuxData(managerRecv, &managerClosed, &managerFrame)
	require.ErrorIs(t, err, io.EOF)
	assert.True(t, managerClosed.Load())
	assert.Nil(t, managerFrame)
	assert.False(t, agentClosed.Load())
}
