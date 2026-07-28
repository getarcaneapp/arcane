package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newWebSocketAgentTunnel(envID string, conn *websocket.Conn) *AgentTunnel {
	return NewAgentTunnelWithConn(envID, NewTunnelConn(conn))
}

func TestTunnelMessage_MarshalJSON(t *testing.T) {
	msg := &TunnelMessage{
		ID:   "test-id",
		Type: MessageTypeRequest,
		Body: nil,
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	// Check that fields are present
	strData := string(data)
	assert.Contains(t, strData, `"id":"test-id"`)
	assert.Contains(t, strData, `"type":"request"`)
}

func TestTunnelConn(t *testing.T) {
	// Start a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			// Echo back
			err = conn.WriteMessage(mt, message)
			if err != nil {
				break
			}
		}
	}))
	defer server.Close()

	// Connect to the server
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()

	tunnelConn := NewTunnelConn(conn)

	// Test Send and Receive
	msg := &TunnelMessage{
		ID:   "test-id",
		Type: MessageTypeRequest,
		Body: []byte("test-body"),
	}

	err = tunnelConn.Send(msg)
	require.NoError(t, err)

	received, err := tunnelConn.Receive()
	require.NoError(t, err)
	assert.Equal(t, msg.ID, received.ID)
	assert.Equal(t, msg.Type, received.Type)
	assert.Equal(t, msg.Body, received.Body)

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
		t.Fatal("timed out waiting for stream recv start")
	}

	require.NoError(t, conn.Close())

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for receive to unblock")
	}
}

type blockingGRPCManagerStream struct {
	ctx         context.Context
	recvStarted chan struct{}
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
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _, err = conn.ReadMessage()
		peerRead <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	tunnelConn := NewTunnelConn(conn)
	require.NoError(t, tunnelConn.Close())

	select {
	case err := <-peerRead:
		assert.True(t, websocket.IsCloseError(err, websocket.CloseNormalClosure),
			"peer should observe a normal closure, got: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for peer read to return")
	}
}

func TestTunnelConn_ReceiveDeadlineMarksClosed(t *testing.T) {
	previousReadWait := tunnelReadWait
	tunnelReadWait = 100 * time.Millisecond
	t.Cleanup(func() { tunnelReadWait = previousReadWait })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Keep the socket open but never send anything.
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	tunnelConn := NewTunnelConn(conn)
	_, err = tunnelConn.Receive()
	require.Error(t, err)
	assert.True(t, tunnelConn.IsClosed())
}

type closeOrderAgentStream struct {
	events []string
}

func (s *closeOrderAgentStream) Send(*tunnelpb.AgentMessage) error { return nil }

func (s *closeOrderAgentStream) Recv() (*tunnelpb.ManagerMessage, error) { return nil, io.EOF }

func (s *closeOrderAgentStream) Context() context.Context { return context.Background() }

func (s *closeOrderAgentStream) CloseSend() error {
	s.events = append(s.events, "close_send")
	return nil
}

func TestGRPCAgentTunnelConn_CloseHalfClosesBeforeCancel(t *testing.T) {
	stream := &closeOrderAgentStream{}
	conn := NewGRPCAgentTunnelConn(stream, func() { stream.events = append(stream.events, "cancel") })

	require.NoError(t, conn.Close())
	assert.Equal(t, []string{"close_send", "cancel"}, stream.events)

	// A second close is a no-op.
	require.NoError(t, conn.Close())
	assert.Equal(t, []string{"close_send", "cancel"}, stream.events)
}
