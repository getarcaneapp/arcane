package edge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Check registry
	reg := GetRegistry()
	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = reg.Get("env-connected")
		return ok && tunnel != nil
	}, time.Second, 10*time.Millisecond)

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
		defer func() { _ = resp.Body.Close() }()
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
		defer func() { _ = resp.Body.Close() }()
	}
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestTunnelConnectRouteRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api")

	server := NewTunnelServer(nil, nil)
	group.GET("/tunnel/connect", server.HandleConnect)

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

func TestTunnelServer_resolveEnvironment_TrimsToken(t *testing.T) {
	var resolvedToken string
	server := NewTunnelServer(func(ctx context.Context, token string) (string, error) {
		resolvedToken = token
		return "env-trimmed", nil
	}, nil)

	envID, err := server.resolveEnvironment(context.Background(), "  valid-token  ")
	require.NoError(t, err)
	assert.Equal(t, "env-trimmed", envID)
	assert.Equal(t, "valid-token", resolvedToken)
}

func TestTunnelServer_resolveEnvironment_Errors(t *testing.T) {
	t.Run("missing resolver", func(t *testing.T) {
		server := NewTunnelServer(nil, nil)
		_, err := server.resolveEnvironment(context.Background(), "token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "edge resolver is not configured")
	})

	t.Run("missing token", func(t *testing.T) {
		server := NewTunnelServer(func(ctx context.Context, token string) (string, error) {
			return "env", nil
		}, nil)

		_, err := server.resolveEnvironment(context.Background(), "   ")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent token required")
	})
}

func TestTokenFromMetadata(t *testing.T) {
	t.Run("prefers agent token and trims whitespace", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
			strings.ToLower(remenv.HeaderAgentToken), "  agent-token  ",
			strings.ToLower(remenv.HeaderAPIKey), "api-token",
		))

		assert.Equal(t, "agent-token", tokenFromMetadata(ctx))
	})

	t.Run("falls back to api key", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
			strings.ToLower(remenv.HeaderAPIKey), "api-token",
		))

		assert.Equal(t, "api-token", tokenFromMetadata(ctx))
	})

	t.Run("returns empty when metadata missing", func(t *testing.T) {
		assert.Equal(t, "", tokenFromMetadata(context.Background()))
	})
}

func TestIsExpectedTunnelReceiveError(t *testing.T) {
	t.Run("nil is not expected", func(t *testing.T) {
		assert.False(t, isExpectedGRPCReceiveErrorInternal(nil))
	})

	t.Run("io eof expected", func(t *testing.T) {
		assert.True(t, isExpectedGRPCReceiveErrorInternal(io.EOF))
	})

	t.Run("context canceled expected", func(t *testing.T) {
		assert.True(t, isExpectedGRPCReceiveErrorInternal(context.Canceled))
	})

	t.Run("context deadline expected", func(t *testing.T) {
		assert.True(t, isExpectedGRPCReceiveErrorInternal(context.DeadlineExceeded))
	})

	t.Run("grpc canceled expected", func(t *testing.T) {
		err := status.Error(codes.Canceled, "context canceled")
		assert.True(t, isExpectedGRPCReceiveErrorInternal(err))
	})

	t.Run("grpc deadline exceeded expected", func(t *testing.T) {
		err := status.Error(codes.DeadlineExceeded, "deadline exceeded")
		assert.True(t, isExpectedGRPCReceiveErrorInternal(err))
	})

	t.Run("unexpected error not expected", func(t *testing.T) {
		assert.False(t, isExpectedGRPCReceiveErrorInternal(errors.New("boom")))
	})
}

func TestTunnelServer_HandleEventCallback(t *testing.T) {
	called := make(chan struct{}, 1)
	server := NewTunnelServer(nil, nil)
	server.SetEventCallback(func(ctx context.Context, environmentID string, event *TunnelEvent) error {
		assert.Equal(t, "env-edge", environmentID)
		require.NotNil(t, event)
		assert.Equal(t, "container.start", event.Type)
		assert.Equal(t, "Container started", event.Title)
		select {
		case called <- struct{}{}:
		default:
		}
		return nil
	})

	tunnel := NewAgentTunnelWithConn("env-edge", &fakeServerTunnelConn{})
	server.handleTunnelMessage(context.Background(), tunnel, &TunnelMessage{
		Type: MessageTypeEvent,
		Event: &TunnelEvent{
			Type:  "container.start",
			Title: "Container started",
		},
	})

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event callback")
	}
}

type fakeServerTunnelConn struct{}

func (f *fakeServerTunnelConn) Send(msg *TunnelMessage) error { return nil }

func (f *fakeServerTunnelConn) Receive() (*TunnelMessage, error) { return nil, io.EOF }

func (f *fakeServerTunnelConn) IsExpectedReceiveError(error) bool { return false }

func (f *fakeServerTunnelConn) Close() error { return nil }

func (f *fakeServerTunnelConn) IsClosed() bool { return false }

func (f *fakeServerTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}
