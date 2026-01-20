package edge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelServer_HandleConnect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token == "valid-token" {
			return "env-connected", nil
		}
		return "", errors.New("invalid token")
	}

	statusCallbackCalled := make(chan struct{}, 1)
	callback := func(ctx context.Context, envID string, connected bool) {
		if envID == "env-connected" && connected {
			select {
			case statusCallbackCalled <- struct{}{}:
			default:
			}
		}
	}

	server := NewTunnelServer(resolver, callback)

	router := gin.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test Success
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"
	headers := http.Header{}
	headers.Set(remenv.HeaderAgentToken, "valid-token")

	conn, resp, err := websocket.DefaultDialer.Dial(url, headers)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Check registry
	reg := GetRegistry()
	tunnel, ok := reg.Get("env-connected")
	assert.True(t, ok)
	assert.NotNil(t, tunnel)

	select {
	case <-statusCallbackCalled:
		// callback observed
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for status callback")
	}

	// Test Heartbeat
	heartbeat := &TunnelMessage{
		ID:   "hb-1",
		Type: MessageTypeHeartbeat,
	}
	err = conn.WriteJSON(heartbeat)
	require.NoError(t, err)

	// Should receive Ack
	var ack TunnelMessage
	err = conn.ReadJSON(&ack)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeHeartbeatAck, ack.Type)
	assert.Equal(t, "hb-1", ack.ID)

	// Test Response Delivery
	// 1. Setup pending request
	respCh := make(chan *TunnelMessage, 1)
	tunnel.Pending.Store("req-1", &PendingRequest{ResponseCh: respCh})

	// 2. Send response from agent
	respMsg := &TunnelMessage{
		ID:   "req-1",
		Type: MessageTypeResponse,
		Body: []byte("response"),
	}
	err = conn.WriteJSON(respMsg)
	require.NoError(t, err)

	// 3. Verify received on channel
	select {
	case received := <-respCh:
		assert.Equal(t, "req-1", received.ID)
		assert.Equal(t, []byte("response"), received.Body)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Test Stream Delivery
	// 1. Setup pending stream
	streamCh := make(chan *TunnelMessage, 1)
	tunnel.Pending.Store("stream-1", &PendingRequest{ResponseCh: streamCh})

	// 2. Send stream data from agent
	streamMsg := &TunnelMessage{
		ID:   "stream-1",
		Type: MessageTypeStreamData,
		Body: []byte("stream"),
	}
	err = conn.WriteJSON(streamMsg)
	require.NoError(t, err)

	// 3. Verify received
	select {
	case received := <-streamCh:
		assert.Equal(t, "stream-1", received.ID)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for stream")
	}

	// Test Ignored/Unknown Messages
	ignoredMsg := &TunnelMessage{ID: "ignore", Type: MessageTypeRequest} // Request coming FROM agent is ignored/unexpected
	_ = conn.WriteJSON(ignoredMsg)

	unknownMsg := &TunnelMessage{ID: "unknown", Type: "unknown_type"}
	_ = conn.WriteJSON(unknownMsg)

	// Allow time for processing
	time.Sleep(100 * time.Millisecond)

	// Clean up
	reg.Unregister("env-connected")
}

func TestTunnelServer_HandleConnect_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resolver := func(ctx context.Context, token string) (string, error) {
		return "", errors.New("invalid")
	}

	server := NewTunnelServer(resolver, nil)
	router := gin.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"
	headers := http.Header{}
	headers.Set(remenv.HeaderAgentToken, "bad-token")

	_, resp, err := websocket.DefaultDialer.Dial(url, headers)
	require.Error(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestTunnelServer_HandleConnect_NoToken(t *testing.T) {
	server := NewTunnelServer(nil, nil)
	router := gin.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"

	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.Error(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestRegisterTunnelRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api")

	RegisterTunnelRoutes(context.Background(), group, nil, nil)

	// Verify route exists (simplistic check by trying to hit it)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tunnel/connect", nil)
	router.ServeHTTP(w, req)

	// Should be 401 because no token, which means the handler was reached
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTunnelServer_CleanupLoop(t *testing.T) {
	server := NewTunnelServer(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// Run cleanup loop
	go server.StartCleanupLoop(ctx)

	// Just ensure it doesn't panic and stops when ctx is cancelled
	time.Sleep(10 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
}
