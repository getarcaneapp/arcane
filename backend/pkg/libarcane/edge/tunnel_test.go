package edge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, websocket.ErrCloseSent, err)
}

func TestTunnelConn_SendRequest(t *testing.T) {
	// Start a test server that responds to requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg TunnelMessage
			_ = json.Unmarshal(message, &msg)

			// Respond
			resp := &TunnelMessage{
				ID:   msg.ID,
				Type: MessageTypeResponse,
				Body: []byte("response"),
			}
			data, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, data)
		}
	}))
	defer server.Close()

	// Connect
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, dialResp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if dialResp != nil {
		defer func() { _ = dialResp.Body.Close() }()
	}

	tunnelConn := NewTunnelConn(conn)
	defer func() { _ = tunnelConn.Close() }()

	// Setup background receiver
	pending := &sync.Map{}
	go func() {
		for {
			msg, err := tunnelConn.Receive()
			if err != nil {
				return
			}
			if req, ok := pending.Load(msg.ID); ok {
				pendingReq := req.(*PendingRequest)
				pendingReq.ResponseCh <- msg
			}
		}
	}()

	// Test SendRequest
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := &TunnelMessage{
		ID:   "req-1",
		Type: MessageTypeRequest,
	}

	resp, err := tunnelConn.SendRequest(ctx, req, pending)
	require.NoError(t, err)
	assert.Equal(t, "req-1", resp.ID)
	assert.Equal(t, MessageTypeResponse, resp.Type)
	assert.Equal(t, []byte("response"), resp.Body)
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
