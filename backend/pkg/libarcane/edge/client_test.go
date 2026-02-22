package edge

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelClient_HandleRequest(t *testing.T) {
	// 1. Setup Local Service (that agent proxies TO)
	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/local/api" {
			w.Header().Set("X-Local", "true")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("local response"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// 2. Setup Mock Manager (that agent connects TO)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer func() { _ = conn.Close() }()

		// Send a request to the agent
		reqMsg := &TunnelMessage{
			ID:     "req-1",
			Type:   MessageTypeRequest,
			Method: "GET",
			Path:   "/local/api",
		}
		data, _ := json.Marshal(reqMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Wait for response
		_, respData, _ := conn.ReadMessage()
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)

		// Validate response from agent
		assert.Equal(t, "req-1", resp.ID)
		assert.Equal(t, MessageTypeResponse, resp.Type)
		assert.Equal(t, http.StatusOK, resp.Status)
		assert.Equal(t, "true", resp.Headers["X-Local"])
		assert.Equal(t, "local response", string(resp.Body))
	}))
	defer managerServer.Close()

	// 3. Configure and Start Agent Client
	cfg := &config.Config{
		EdgeTransport:         EdgeTransportWebSocket,
		ManagerApiUrl:         managerServer.URL,
		AgentToken:            "test-token",
		EdgeReconnectInterval: 1,
	}

	client := NewTunnelClient(cfg, localHandler)
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx := t.Context()

	// Run client in background
	go client.StartWithErrorChan(ctx, nil)

	// Wait for process to finish or timeout
	time.Sleep(100 * time.Millisecond)
}

func TestTunnelClient_WebSocketProxy(t *testing.T) {
	// 1. Setup Local Service with WS
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo
			_ = conn.WriteMessage(mt, append([]byte("local echo: "), data...))
		}
	}))
	defer localServer.Close()

	localPort := strings.Split(localServer.Listener.Addr().String(), ":")[1]

	// 2. Setup Mock Manager
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer func() { _ = conn.Close() }()

		// Send WS Start
		startMsg := &TunnelMessage{
			ID:   "ws-1",
			Type: MessageTypeWebSocketStart,
			Path: "/", // Connect to root of local server
		}
		data, _ := json.Marshal(startMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Send Data
		dataMsg := &TunnelMessage{
			ID:            "ws-1",
			Type:          MessageTypeWebSocketData,
			Body:          []byte("hello"),
			WSMessageType: websocket.TextMessage,
		}
		data, _ = json.Marshal(dataMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Read Echo
		_, respData, _ := conn.ReadMessage()
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)

		assert.Equal(t, MessageTypeWebSocketData, resp.Type)
		assert.Equal(t, "local echo: hello", string(resp.Body))
	}))
	defer managerServer.Close()

	// 3. Configure Agent
	cfg := &config.Config{
		EdgeTransport: EdgeTransportWebSocket,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "test-token",
		Port:          localPort, // Tell agent where local service is
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler()) // Handler ignored for WS
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx := t.Context()

	go client.StartWithErrorChan(ctx, nil)
	time.Sleep(100 * time.Millisecond)
}

func TestTunnelClient_HandleRequest_Errors(t *testing.T) {
	// Setup Mock Manager
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer func() { _ = conn.Close() }()

		// 1. Send request with invalid URL to trigger error
		reqMsg := &TunnelMessage{
			ID:     "req-err",
			Type:   MessageTypeRequest,
			Method: "GET",
			Path:   "://invalid-url",
		}
		data, _ := json.Marshal(reqMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Expect error response
		_, respData, _ := conn.ReadMessage()
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)

		assert.Equal(t, "req-err", resp.ID)
		assert.Equal(t, 500, resp.Status)

		// 2. Send unknown message type
		unknownMsg := &TunnelMessage{
			ID:   "unknown",
			Type: "unknown_type",
		}
		data, _ = json.Marshal(unknownMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}))
	defer managerServer.Close()

	cfg := &config.Config{
		EdgeTransport: EdgeTransportWebSocket,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "test-token",
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx := t.Context()

	go client.StartWithErrorChan(ctx, nil)
	time.Sleep(100 * time.Millisecond)
}

func TestTunnelClient_InternalHelpers(t *testing.T) {
	// Mock connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer func() { _ = conn.Close() }()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		ManagerApiUrl: server.URL,
		AgentToken:    "test-token",
	}
	client := NewTunnelClient(cfg, nil)

	// Manually connect
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()

	client.conn = NewTunnelConn(conn)

	// Test sendWebSocketData
	err = client.sendWebSocketData("stream-1", websocket.TextMessage, []byte("data"))
	require.NoError(t, err)

	// Test sendWebSocketClose
	client.sendWebSocketClose("stream-1")

	// Test sendErrorResponse
	client.sendErrorResponse("req-1", 500, "error")
}

func TestTunnelClient_BuildLocalWebSocketURL(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		port     string
		path     string
		query    string
		expected string
	}{
		{
			name:     "empty listen uses localhost",
			listen:   "",
			port:     "3553",
			path:     "/api",
			query:    "",
			expected: "ws://localhost:3553/api",
		},
		{
			name:     "wildcard ipv4 maps to localhost",
			listen:   "0.0.0.0",
			port:     "3553",
			path:     "/",
			query:    "",
			expected: "ws://localhost:3553/",
		},
		{
			name:     "wildcard ipv6 maps to localhost",
			listen:   "::",
			port:     "3553",
			path:     "/",
			query:    "",
			expected: "ws://localhost:3553/",
		},
		{
			name:     "explicit ipv4 listen",
			listen:   "127.0.0.1",
			port:     "3553",
			path:     "/",
			query:    "q=1",
			expected: "ws://127.0.0.1:3553/?q=1",
		},
		{
			name:     "explicit ipv6 listen",
			listen:   "2001:db8::1",
			port:     "3553",
			path:     "/ws",
			query:    "",
			expected: "ws://[2001:db8::1]:3553/ws",
		},
		{
			name:     "listen host and port wildcard maps to localhost",
			listen:   "0.0.0.0:3553",
			port:     "3553",
			path:     "/ws",
			query:    "",
			expected: "ws://localhost:3553/ws",
		},
		{
			name:     "listen with port only maps to localhost",
			listen:   ":3553",
			port:     "3553",
			path:     "/ws",
			query:    "",
			expected: "ws://localhost:3553/ws",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := &config.Config{
				Listen: testCase.listen,
				Port:   testCase.port,
			}
			client := NewTunnelClient(cfg, http.NotFoundHandler())
			msg := &TunnelMessage{
				Path:  testCase.path,
				Query: testCase.query,
			}
			assert.Equal(t, testCase.expected, client.buildLocalWebSocketURLInternal(msg))
		})
	}
}

func TestTunnelClient_GRPCConnectMethodInternal(t *testing.T) {
	client := NewTunnelClient(&config.Config{}, http.NotFoundHandler())
	assert.Equal(t, "/api/tunnel/connect", client.grpcConnectMethodInternal())
}

func TestTunnelClient_buildLocalWebSocketHeadersInternal(t *testing.T) {
	client := NewTunnelClient(&config.Config{
		AgentToken: "agent-token",
	}, http.NotFoundHandler())

	headers := client.buildLocalWebSocketHeadersInternal(&TunnelMessage{
		Headers: map[string]string{
			"sec-websocket-key":      "abc",
			"sec-websocket-version":  "13",
			"Sec-WebSocket-Protocol": "binary",
			"X-Custom":               "value",
			"X-API-Key":              "manager-token",
		},
	})

	assert.Empty(t, headers.Get("Sec-Websocket-Key"))
	assert.Empty(t, headers.Get("Sec-Websocket-Version"))
	assert.Equal(t, "binary", headers.Get("Sec-Websocket-Protocol"))
	assert.Equal(t, "value", headers.Get("X-Custom"))
	assert.Equal(t, "agent-token", headers.Get("X-API-Key"))
	assert.Equal(t, "agent-token", headers.Get("X-Arcane-Agent-Token"))
}

func TestTunnelClient_IsGRPCConnectionInternal(t *testing.T) {
	t.Run("nil connection", func(t *testing.T) {
		client := &TunnelClient{}
		assert.False(t, client.isGRPCConnectionInternal())
	})

	t.Run("grpc connection", func(t *testing.T) {
		client := &TunnelClient{conn: NewGRPCAgentTunnelConn(nil)}
		assert.True(t, client.isGRPCConnectionInternal())
	})

	t.Run("non-grpc connection", func(t *testing.T) {
		client := &TunnelClient{conn: &fakeTunnelConnForTransportCheck{}}
		assert.False(t, client.isGRPCConnectionInternal())
	})
}

func TestTunnelClient_HandleRequest_GRPCConfigWithWebSocketConnUsesNonStreamingResponse(t *testing.T) {
	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/local/api", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	client := NewTunnelClient(&config.Config{
		EdgeTransport: EdgeTransportGRPC,
	}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.conn = conn

	client.handleRequest(context.Background(), &TunnelMessage{
		ID:     "req-fallback-1",
		Type:   MessageTypeRequest,
		Method: http.MethodGet,
		Path:   "/local/api",
	})

	require.Len(t, conn.sent, 1)
	assert.Equal(t, MessageTypeResponse, conn.sent[0].Type)
	assert.Equal(t, http.StatusOK, conn.sent[0].Status)
	assert.Equal(t, `{"ok":true}`, string(conn.sent[0].Body))
}

func TestTunnelClient_HeartbeatLoop_ClosesConnectionOnSendFailure(t *testing.T) {
	conn := &failingHeartbeatConn{}
	client := &TunnelClient{
		conn:              conn,
		heartbeatInterval: 5 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client.heartbeatLoop(ctx)
	assert.True(t, conn.closeCalled)
}

type fakeTunnelConnForTransportCheck struct{}

func (f *fakeTunnelConnForTransportCheck) Send(_ *TunnelMessage) error {
	return nil
}

func (f *fakeTunnelConnForTransportCheck) Receive() (*TunnelMessage, error) {
	return nil, nil
}

func (f *fakeTunnelConnForTransportCheck) IsExpectedReceiveError(error) bool {
	return false
}

func (f *fakeTunnelConnForTransportCheck) Close() error {
	return nil
}

func (f *fakeTunnelConnForTransportCheck) IsClosed() bool {
	return false
}

func (f *fakeTunnelConnForTransportCheck) SendRequest(context.Context, *TunnelMessage, *sync.Map) (*TunnelMessage, error) {
	return nil, nil
}

type capturingTunnelConnForHandleRequest struct {
	sent []*TunnelMessage
}

func (c *capturingTunnelConnForHandleRequest) Send(msg *TunnelMessage) error {
	cloned := *msg
	if msg.Headers != nil {
		cloned.Headers = make(map[string]string, len(msg.Headers))
		maps.Copy(cloned.Headers, msg.Headers)
	}
	if msg.Body != nil {
		cloned.Body = append([]byte(nil), msg.Body...)
	}
	c.sent = append(c.sent, &cloned)
	return nil
}

func (c *capturingTunnelConnForHandleRequest) Receive() (*TunnelMessage, error) {
	return nil, nil
}

func (c *capturingTunnelConnForHandleRequest) IsExpectedReceiveError(error) bool {
	return false
}

func (c *capturingTunnelConnForHandleRequest) Close() error {
	return nil
}

func (c *capturingTunnelConnForHandleRequest) IsClosed() bool {
	return false
}

func (c *capturingTunnelConnForHandleRequest) SendRequest(context.Context, *TunnelMessage, *sync.Map) (*TunnelMessage, error) {
	return nil, nil
}

type failingHeartbeatConn struct {
	closeCalled bool
}

func (f *failingHeartbeatConn) Send(*TunnelMessage) error {
	return errors.New("send failed")
}

func (f *failingHeartbeatConn) Receive() (*TunnelMessage, error) {
	return nil, errors.New("receive not implemented")
}

func (f *failingHeartbeatConn) IsExpectedReceiveError(error) bool {
	return false
}

func (f *failingHeartbeatConn) Close() error {
	f.closeCalled = true
	return nil
}

func (f *failingHeartbeatConn) IsClosed() bool {
	return false
}

func (f *failingHeartbeatConn) SendRequest(context.Context, *TunnelMessage, *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}
