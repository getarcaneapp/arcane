package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	httputil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	slogecho "github.com/samber/slog-echo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
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

		_, registerData, _ := conn.ReadMessage()
		var registerMsg TunnelMessage
		_ = json.Unmarshal(registerData, &registerMsg)
		assert.Equal(t, MessageTypeRegister, registerMsg.Type)
		registerResp, _ := json.Marshal(&TunnelMessage{
			Type:      MessageTypeRegisterResponse,
			Accepted:  true,
			SessionID: "session-1",
		})
		_ = conn.WriteMessage(websocket.TextMessage, registerResp)

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
	cfg := &Config{
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

		_, registerData, _ := conn.ReadMessage()
		var registerMsg TunnelMessage
		_ = json.Unmarshal(registerData, &registerMsg)
		assert.Equal(t, MessageTypeRegister, registerMsg.Type)
		registerResp, _ := json.Marshal(&TunnelMessage{
			Type:      MessageTypeRegisterResponse,
			Accepted:  true,
			SessionID: "session-1",
		})
		_ = conn.WriteMessage(websocket.TextMessage, registerResp)

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
	cfg := &Config{
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

// TestTunnelClient_WebSocket_ReconnectClosesStreams verifies that a reconnect
// reclaims the goroutines, local sockets, and activeStreams entries opened on
// the previous connection instead of leaking them. Run under -race it also
// exercises concurrent c.conn access across the reassignment on reconnect.
func TestTunnelClient_WebSocket_ReconnectClosesStreams(t *testing.T) {
	// Local WS echo server the agent proxies to.
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
			_ = conn.WriteMessage(mt, append([]byte("local echo: "), data...))
		}
	}))
	defer localServer.Close()
	localPort := strings.Split(localServer.Listener.Addr().String(), ":")[1]

	var connCount atomic.Int32
	firstStreamLive := make(chan struct{})
	closeFirstConn := make(chan struct{})
	stopManager := make(chan struct{})

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Every connection registers first.
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		registerResp, _ := json.Marshal(&TunnelMessage{
			Type:      MessageTypeRegisterResponse,
			Accepted:  true,
			SessionID: "session-1",
		})
		_ = conn.WriteMessage(websocket.TextMessage, registerResp)

		if connCount.Add(1) > 1 {
			// Post-reconnect connection: stay up so the agent stops reconnecting
			// and the test can assert a stable, empty stream set.
			<-stopManager
			return
		}

		// First connection: open a stream and prove it is live by round-tripping
		// data through the local echo server, then force a disconnect.
		startMsg, _ := json.Marshal(&TunnelMessage{ID: "ws-1", Type: MessageTypeWebSocketStart, Path: "/"})
		_ = conn.WriteMessage(websocket.TextMessage, startMsg)
		dataMsg, _ := json.Marshal(&TunnelMessage{
			ID:            "ws-1",
			Type:          MessageTypeWebSocketData,
			Body:          []byte("hello"),
			WSMessageType: websocket.TextMessage,
		})
		_ = conn.WriteMessage(websocket.TextMessage, dataMsg)

		_, respData, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)
		assert.Equal(t, MessageTypeWebSocketData, resp.Type)
		assert.Equal(t, "local echo: hello", string(resp.Body))

		close(firstStreamLive)
		<-closeFirstConn // returning closes the conn, forcing the agent to reconnect
	}))
	defer managerServer.Close()

	cfg := &Config{
		EdgeTransport: EdgeTransportWebSocket,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "test-token",
		Port:          localPort,
	}
	client := NewTunnelClient(cfg, http.NotFoundHandler())
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")
	client.reconnectInterval = 50 * time.Millisecond

	countStreams := func() int {
		n := 0
		client.activeStreams.Range(func(_, _ any) bool { n++; return true })
		return n
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(stopManager)
	go client.StartWithErrorChan(ctx, nil)

	// The stream is registered while the first connection is live.
	select {
	case <-firstStreamLive:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first stream to come up")
	}
	assert.Equal(t, 1, countStreams(), "expected one active stream during the first connection")

	// Force the disconnect; the reconnect must reclaim the stream.
	close(closeFirstConn)
	require.Eventually(t, func() bool {
		return connCount.Load() >= 2 && countStreams() == 0
	}, 5*time.Second, 20*time.Millisecond, "stream leaked across reconnect")
}

func TestTunnelClient_HandleRequest_Errors(t *testing.T) {
	// Setup Mock Manager
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer func() { _ = conn.Close() }()

		_, registerData, _ := conn.ReadMessage()
		var registerMsg TunnelMessage
		_ = json.Unmarshal(registerData, &registerMsg)
		assert.Equal(t, MessageTypeRegister, registerMsg.Type)
		registerResp, _ := json.Marshal(&TunnelMessage{
			Type:      MessageTypeRegisterResponse,
			Accepted:  true,
			SessionID: "session-1",
		})
		_ = conn.WriteMessage(websocket.TextMessage, registerResp)

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

	cfg := &Config{
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

	cfg := &Config{
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

	tunnelConn := NewTunnelConn(conn)
	client.setConn(tunnelConn)

	// Test sendWebSocketData
	err = client.sendWebSocketData(tunnelConn, "stream-1", websocket.TextMessage, []byte("data"))
	require.NoError(t, err)

	// Test sendWebSocketClose
	client.sendWebSocketClose(tunnelConn, "stream-1")

	// Test sendErrorResponse
	client.sendErrorResponse(tunnelConn, "req-1", 500, "error")
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
			cfg := &Config{
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
	client := NewTunnelClient(&Config{}, http.NotFoundHandler())
	assert.Equal(t, "/api/tunnel/connect", client.grpcConnectMethodInternal())
}

func TestTunnelClient_buildLocalWebSocketHeadersInternal(t *testing.T) {
	client := NewTunnelClient(&Config{
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

func TestTunnelClient_buildLocalWebSocketHeadersInternal_FiltersBrowserHeaders(t *testing.T) {
	client := NewTunnelClient(&Config{
		AgentToken: "agent-token",
	}, http.NotFoundHandler())

	headers := client.buildLocalWebSocketHeadersInternal(&TunnelMessage{
		Headers: map[string]string{
			// Browser headers that should be stripped
			"Origin":             "https://docker.example.com",
			"Cookie":             "session=abc123",
			"Authorization":      "Bearer browser-jwt",
			"Referer":            "https://docker.example.com/environments/123",
			"Sec-Fetch-Dest":     "websocket",
			"Sec-Fetch-Mode":     "websocket",
			"Sec-Fetch-Site":     "same-origin",
			"Sec-Fetch-User":     "?1",
			"Sec-Ch-Ua":          "\"Chromium\";v=\"130\"",
			"Sec-Ch-Ua-Mobile":   "?0",
			"Sec-Ch-Ua-Platform": "\"Linux\"",
			// Headers that should be preserved
			"X-Custom":               "value",
			"Sec-WebSocket-Protocol": "binary",
			"Accept-Language":        "en-US",
		},
	})

	// Browser headers must be stripped
	assert.Empty(t, headers.Get("Origin"), "Origin should be stripped")
	assert.Empty(t, headers.Get("Cookie"), "Cookie should be stripped")
	assert.Empty(t, headers.Get("Authorization"), "Authorization should be stripped")
	assert.Empty(t, headers.Get("Referer"), "Referer should be stripped")
	assert.Empty(t, headers.Get("Sec-Fetch-Dest"), "Sec-Fetch-Dest should be stripped")
	assert.Empty(t, headers.Get("Sec-Fetch-Mode"), "Sec-Fetch-Mode should be stripped")
	assert.Empty(t, headers.Get("Sec-Fetch-Site"), "Sec-Fetch-Site should be stripped")
	assert.Empty(t, headers.Get("Sec-Fetch-User"), "Sec-Fetch-User should be stripped")
	assert.Empty(t, headers.Get("Sec-Ch-Ua"), "Sec-Ch-Ua should be stripped")
	assert.Empty(t, headers.Get("Sec-Ch-Ua-Mobile"), "Sec-Ch-Ua-Mobile should be stripped")
	assert.Empty(t, headers.Get("Sec-Ch-Ua-Platform"), "Sec-Ch-Ua-Platform should be stripped")

	// Non-browser headers must be preserved
	assert.Equal(t, "value", headers.Get("X-Custom"))
	assert.Equal(t, "binary", headers.Get("Sec-Websocket-Protocol"))
	assert.Equal(t, "en-US", headers.Get("Accept-Language"))

	// Agent token must override any manager-forwarded auth
	assert.Equal(t, "agent-token", headers.Get("X-API-Key"))
	assert.Equal(t, "agent-token", headers.Get("X-Arcane-Agent-Token"))
}

func TestTunnelClient_DialLocalWebSocket_StripsForwardedBrowserHeaders(t *testing.T) {
	managerURL := "https://manager.internal.example.com"

	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "agent-token" {
			http.Error(w, "missing agent auth", http.StatusForbidden)
			return
		}

		if origin := r.Header.Get("Origin"); origin != "" {
			http.Error(w, "unexpected forwarded origin: "+origin, http.StatusForbidden)
			return
		}

		if cookie := r.Header.Get("Cookie"); cookie != "" {
			http.Error(w, "unexpected forwarded cookie", http.StatusForbidden)
			return
		}

		upgrader := websocket.Upgrader{
			CheckOrigin: httputil.ValidateWebSocketOrigin(managerURL),
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("ok")))
	}))
	defer localServer.Close()

	parsedURL, err := url.Parse(localServer.URL)
	require.NoError(t, err)

	client := NewTunnelClient(&Config{
		AgentToken: "agent-token",
		Listen:     parsedURL.Hostname(),
		Port:       parsedURL.Port(),
	}, http.NotFoundHandler())

	msg := &TunnelMessage{
		Path: "/",
		Headers: map[string]string{
			"Host":              "manager.example.com",
			"Origin":            "https://public.browser.example.com",
			"Cookie":            "session=browser-cookie",
			"Authorization":     "Bearer browser-token",
			"Sec-Fetch-Mode":    "websocket",
			"Sec-Fetch-Site":    "same-origin",
			"Sec-Websocket-Key": "forwarded-handshake-key",
		},
	}

	headers := client.buildLocalWebSocketHeadersInternal(msg)
	assert.Empty(t, headers.Get("Host"))
	assert.Empty(t, headers.Get("Origin"))
	assert.Empty(t, headers.Get("Cookie"))
	assert.Empty(t, headers.Get("Authorization"))

	ws, resp, err := client.dialLocalWebSocket(t.Context(), client.buildLocalWebSocketURLInternal(msg), headers)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	msgType, body, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, msgType)
	assert.Equal(t, "ok", string(body))
}

func TestTunnelClient_IsGRPCConnectionInternal(t *testing.T) {
	t.Run("nil connection", func(t *testing.T) {
		assert.False(t, isGRPCConnection(nil))
	})

	t.Run("grpc connection", func(t *testing.T) {
		assert.True(t, isGRPCConnection(NewGRPCAgentTunnelConn(nil)))
	})

	t.Run("non-grpc connection", func(t *testing.T) {
		assert.False(t, isGRPCConnection(&fakeTunnelConnForTransportCheck{}))
	})
}

func TestTunnelClient_HandleRequest_GRPCConfigWithWebSocketConnUsesNonStreamingResponse(t *testing.T) {
	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/local/api", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
	}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.setConn(conn)

	client.handleRequest(context.Background(), conn, &TunnelMessage{
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
		heartbeatInterval: 5 * time.Millisecond,
	}
	client.setConn(conn)

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

type stallingTunnelService struct {
	tunnelpb.UnimplementedTunnelServiceServer
	connectCount atomic.Int32
}

type blockingRegistrationConn struct {
	closeCount       atomic.Int32
	closedCh         chan struct{}
	receiveStarted   chan struct{}
	receiveStartOnce sync.Once
}

func newBlockingRegistrationConnInternal() *blockingRegistrationConn {
	return &blockingRegistrationConn{
		closedCh:       make(chan struct{}),
		receiveStarted: make(chan struct{}),
	}
}

func (c *blockingRegistrationConn) Send(*TunnelMessage) error { return nil }

func (c *blockingRegistrationConn) Receive() (*TunnelMessage, error) {
	c.receiveStartOnce.Do(func() {
		close(c.receiveStarted)
	})
	<-c.closedCh
	return nil, io.EOF
}

func (c *blockingRegistrationConn) IsExpectedReceiveError(error) bool { return false }

func (c *blockingRegistrationConn) Close() error {
	if c.closeCount.Add(1) == 1 {
		close(c.closedCh)
	}
	return nil
}

func (c *blockingRegistrationConn) IsClosed() bool {
	select {
	case <-c.closedCh:
		return true
	default:
		return false
	}
}

func (c *blockingRegistrationConn) SendRequest(context.Context, *TunnelMessage, *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func (s *stallingTunnelService) Connect(stream grpc.BidiStreamingServer[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]) error {
	s.connectCount.Add(1)
	if _, err := stream.Recv(); err != nil {
		return err
	}

	<-stream.Context().Done()
	return stream.Context().Err()
}

func TestTunnelClient_GRPC_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	envID := "env-e2e-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-from-agent"))
	})

	cfg := &Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         managerURL,
		AgentToken:            "valid-token",
		EdgeReconnectInterval: 1,
		Port:                  "3552",
	}

	client := NewTunnelClient(cfg, localHandler)
	errCh := make(chan error, 4)
	go client.StartWithErrorChan(ctx, errCh)

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, 5*time.Second, 20*time.Millisecond)

	proxyCtx, proxyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer proxyCancel()

	status, headers, body, err := ProxyRequest(proxyCtx, tunnel, http.MethodGet, "/api/health", "", map[string]string{"Accept": "text/plain"}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "text/plain", headers["Content-Type"])
	assert.Equal(t, "ok-from-agent", string(body))

	select {
	case clientErr := <-errCh:
		require.NoError(t, clientErr)
	default:
	}
}

func TestTunnelClient_useTLSForManagerGRPC(t *testing.T) {
	tests := []struct {
		name       string
		managerURL string
		expected   bool
	}{
		{name: "https manager url", managerURL: "https://manager.example.com/api", expected: true},
		{name: "https manager url with reverse proxy path", managerURL: "https://manager.example.com/arcane/api", expected: true},
		{name: "http manager url", managerURL: "http://manager.example.com/api", expected: false},
		{name: "invalid manager url", managerURL: "://bad-url", expected: false},
		{name: "empty manager url", managerURL: "", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewTunnelClient(&Config{ManagerApiUrl: tc.managerURL}, http.NotFoundHandler())
			assert.Equal(t, tc.expected, client.useTLSForManagerGRPC())
		})
	}
}

func TestStartTunnelClientWithErrors_GRPCValidation(t *testing.T) {
	ctx := t.Context()

	t.Run("edge mode required", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "edge tunnel disabled")
	})

	t.Run("manager url required for grpc transport", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			AgentToken:    "token",
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "MANAGER_API_URL")
	})

	t.Run("agent token required", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			ManagerApiUrl: "https://manager.example.com/arcane/api",
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "AGENT_TOKEN is required")
	})

	t.Run("mtls requires https manager url", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			ManagerApiUrl: "http://manager.example.com/api",
			AgentToken:    "token",
			EdgeMTLSMode:  EdgeMTLSModeRequired,
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "MANAGER_API_URL to use https")
	})

	t.Run("required mtls auto-enrollment failure surfaces", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			ManagerApiUrl: "https://127.0.0.1:1/api",
			AgentToken:    "token",
			EdgeMTLSMode:  EdgeMTLSModeRequired,
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "edge mTLS enrollment request failed")
	})
}

func TestTunnelClient_connectAndServeGRPC_EmptyManagerAddress(t *testing.T) {
	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	err := client.connectAndServeGRPC(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager gRPC address is empty")
}

func TestTunnelClient_connectAndServeGRPC_RegistrationRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	envID := "env-e2e-grpc-reject-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         managerURL,
		AgentToken:            "invalid-token",
		EdgeReconnectInterval: 1,
		Port:                  "3552",
	}, http.NotFoundHandler())

	err := client.connectAndServeGRPC(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager rejected tunnel registration")
	assert.Contains(t, err.Error(), "invalid agent token")
}

func TestTunnelClient_connectAndServeGRPC_TimesOutWithoutRegisterResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	service := &stallingTunnelService{}
	managerURL, stopManager := startTestTunnelServiceOnAPIPathInternal(t, ctx, service)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	err := client.connectAndServeGRPC(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out waiting for tunnel registration response")
	assert.EqualValues(t, 1, service.connectCount.Load())
	assert.True(t, client.getConn() == nil || client.getConn().IsClosed())
}

func TestTunnelClient_awaitGRPCRegistrationInternal_ClosesConnOnContextDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	conn := newBlockingRegistrationConnInternal()
	client := &TunnelClient{
		grpcRegistrationTimeout: time.Second,
	}
	client.setConn(conn)

	msg, err := client.awaitGRPCRegistrationInternal(ctx)
	require.Nil(t, msg)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, conn.IsClosed())
	assert.EqualValues(t, 1, conn.closeCount.Load())
}

func TestTunnelClient_awaitGRPCRegistrationInternal_ClosesAttemptConnWhenClientConnChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstConn := newBlockingRegistrationConnInternal()
	secondConn := newBlockingRegistrationConnInternal()
	t.Cleanup(func() {
		_ = firstConn.Close()
		_ = secondConn.Close()
	})

	client := &TunnelClient{
		grpcRegistrationTimeout: time.Second,
	}
	client.setConn(firstConn)

	type registrationResult struct {
		msg *TunnelMessage
		err error
	}
	resultCh := make(chan registrationResult, 1)
	go func() {
		msg, err := client.awaitGRPCRegistrationInternal(ctx)
		resultCh <- registrationResult{msg: msg, err: err}
	}()

	select {
	case <-firstConn.receiveStarted:
	case <-time.After(time.Second):
		t.Fatal("registration receive did not start")
	}

	client.setConn(secondConn)
	cancel()

	select {
	case result := <-resultCh:
		require.Nil(t, result.msg)
		require.ErrorIs(t, result.err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("registration did not exit after cancellation")
	}

	assert.True(t, firstConn.IsClosed())
	assert.EqualValues(t, 1, firstConn.closeCount.Load())
	assert.False(t, secondConn.IsClosed())
	assert.EqualValues(t, 0, secondConn.closeCount.Load())
}

func TestTunnelClient_GRPC_WebSocketProxyEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	envID := "env-e2e-grpc-ws-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	headerTokenCh := make(chan string, 1)
	queryCh := make(chan string, 1)
	pathCh := make(chan string, 1)
	receivedMsgCh := make(chan string, 1)
	upgradeErrCh := make(chan string, 1)

	localWSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case pathCh <- r.URL.Path:
		default:
		}
		select {
		case headerTokenCh <- r.Header.Get(HeaderAPIKey):
		default:
		}
		select {
		case queryCh <- r.URL.RawQuery:
		default:
		}

		upgrader := websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
			Error: func(_ http.ResponseWriter, _ *http.Request, status int, reason error) {
				select {
				case upgradeErrCh <- reason.Error():
				default:
				}
			},
		}
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
			select {
			case receivedMsgCh <- string(data):
			default:
			}
			if err := conn.WriteMessage(mt, append([]byte("local echo: "), data...)); err != nil {
				return
			}
		}
	}))
	defer localWSServer.Close()

	localHost, localPort, err := net.SplitHostPort(localWSServer.Listener.Addr().String())
	require.NoError(t, err)
	localHost = strings.Trim(localHost, "[]")

	cfg := &Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         managerURL,
		AgentToken:            "valid-token",
		EdgeReconnectInterval: 1,
		Listen:                localHost,
		Port:                  localPort,
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	errCh := make(chan error, 4)
	go client.StartWithErrorChan(ctx, errCh)

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, 5*time.Second, 20*time.Millisecond)

	router := echo.New()
	router.GET("/proxy-ws", func(c echo.Context) error {
		tunnel, ok := GetRegistry().Get(envID)
		if !ok || tunnel == nil {
			return c.NoContent(http.StatusServiceUnavailable)
		}
		return ProxyWebSocketRequest(c, tunnel, "/api/environments/0/ws/system/stats")
	})

	proxyServer := httptest.NewServer(router)
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/proxy-ws?tail=100"
	proxyConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = proxyConn.Close() }()

	require.NoError(t, proxyConn.WriteMessage(websocket.TextMessage, []byte("hello-grpc-ws")))

	msgType, payload, err := proxyConn.ReadMessage()
	select {
	case upgradeErr := <-upgradeErrCh:
		t.Fatalf("local websocket upgrade failed: %s", upgradeErr)
	default:
	}
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, msgType)
	assert.Equal(t, "local echo: hello-grpc-ws", string(payload))

	select {
	case got := <-pathCh:
		assert.Equal(t, "/api/environments/0/ws/system/stats", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for forwarded websocket path")
	}

	select {
	case got := <-headerTokenCh:
		assert.Equal(t, "valid-token", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for local websocket auth header")
	}

	select {
	case got := <-queryCh:
		assert.Equal(t, "tail=100", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for forwarded query")
	}

	select {
	case got := <-receivedMsgCh:
		assert.Equal(t, "hello-grpc-ws", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for forwarded websocket payload")
	}

	select {
	case clientErr := <-errCh:
		require.NoError(t, clientErr)
	default:
	}
}

func TestTunnelClient_connectAndServe_WebSocketConfigFallsBackToWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		select {
		case wsConnectedCh <- struct{}{}:
		default:
		}

		time.Sleep(100 * time.Millisecond)
	}))
	defer managerServer.Close()

	cfg := &Config{
		EdgeTransport: EdgeTransportWebSocket,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	err := client.connectAndServe(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected websocket fallback connection to manager")
	}
}

func TestTunnelClient_managedTunnelTransports_AutoEnablesGRPCAndWebSocket(t *testing.T) {
	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: "http://manager.example.com",
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	transports := client.managedTunnelTransportsInternal()
	assert.True(t, transports.grpc)
	assert.True(t, transports.websocket)
}

func TestTunnelClient_connectAndServe_AutoFallsBackToWebSocketWhenGRPCUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	managerURL, wsConnectedCh, stopManager := startTestWebSocketTunnelManagerInternal(t, ctx)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	grpcAddr, releaseGRPCAddr := reserveTCPAddressInternal(t)
	defer releaseGRPCAddr()
	client.managerGRPCAddr = grpcAddr
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServe(ctx)
	}()

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected auto transport to fall back to websocket when gRPC is unavailable")
	}

	cancel()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for auto fallback tunnel shutdown")
	}
}

func TestTunnelClient_connectAndServe_AutoFallsBackToWebSocketWhenGRPCSetupHangs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	grpcAddr, stopGRPC := startHangingTCPServerInternal(t, ctx)
	defer stopGRPC()

	managerURL, wsConnectedCh, stopManager := startTestWebSocketTunnelManagerInternal(t, ctx)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.managerGRPCAddr = grpcAddr
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServe(ctx)
	}()

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected auto transport to fall back to websocket when gRPC setup hangs")
	}

	cancel()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for auto fallback tunnel shutdown")
	}
}

func TestTunnelClient_connectAndServe_GRPCDoesNotFallbackToWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	managerURL, wsConnectedCh, stopManager := startTestWebSocketTunnelManagerInternal(t, ctx)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	grpcAddr, releaseGRPCAddr := reserveTCPAddressInternal(t)
	defer releaseGRPCAddr()
	client.managerGRPCAddr = grpcAddr
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	err := client.connectAndServe(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
		t.Fatal("explicit gRPC transport should not fall back to websocket")
	default:
	}
}

func TestTunnelClient_connectAndServe_OpensGRPCWhenAvailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	envID := "env-auto-poll-grpc"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServe(ctx)
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
			return false
		}
		_, isGRPC := tunnel.Conn.(*GRPCManagerTunnelConn)
		return isGRPC
	}, 3*time.Second, 20*time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for gRPC tunnel shutdown")
	}
}

func startTestWebSocketTunnelManagerInternal(t *testing.T, ctx context.Context) (string, <-chan struct{}, func()) {
	t.Helper()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}

		registerResp, err := json.Marshal(&TunnelMessage{
			Type:          MessageTypeRegisterResponse,
			Accepted:      true,
			EnvironmentID: "env-ws-fallback",
			SessionID:     "session-ws-fallback",
		})
		if err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, registerResp); err != nil {
			return
		}

		select {
		case wsConnectedCh <- struct{}{}:
		default:
		}

		<-ctx.Done()
	}))

	return managerServer.URL, wsConnectedCh, managerServer.Close
}

func reserveTCPAddressInternal(t *testing.T) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return lis.Addr().String(), func() {
		require.NoError(t, lis.Close())
	}
}

func startHangingTCPServerInternal(t *testing.T, ctx context.Context) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				<-ctx.Done()
			}()
		}
	}()

	stop := func() {
		_ = lis.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for hanging TCP server shutdown")
		}
	}

	return lis.Addr().String(), stop
}

func startTestGRPCTunnelServerOnAPIPathInternal(t *testing.T, ctx context.Context, tunnelServer *TunnelServer) (string, func()) {
	t.Helper()
	return startTestTunnelServiceOnAPIPathInternal(t, ctx, tunnelServer)
}

func startTestTunnelServiceOnAPIPathInternal(t *testing.T, ctx context.Context, service tunnelpb.TunnelServiceServer) (string, func()) {
	t.Helper()

	grpcServer := grpc.NewServer()
	tunnelpb.RegisterTunnelServiceServer(grpcServer, service)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clone := r.Clone(r.Context())
		cloneURL := *clone.URL
		if r.URL.Path == "/api/tunnel/connect" {
			cloneURL.Path = tunnelpb.TunnelService_Connect_FullMethodName
		} else {
			cloneURL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		}
		clone.URL = &cloneURL
		clone.RequestURI = cloneURL.Path
		grpcServer.ServeHTTP(w, clone)
	}), &http2.Server{})

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = httpServer.Serve(lis)
	}()

	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		grpcServer.Stop()
		_ = lis.Close()
	}

	return "http://" + lis.Addr().String(), cleanup
}

func TestTunnelClient_connectAndServePoll_OpensGRPCWhenRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	envID := "env-poll-grpc"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	managerURL, stopManager := startTestPollAndGRPCManagerInternal(t, ctx, tunnelServer, TunnelPollResponse{
		Status:              TunnelStatusRequired,
		PollIntervalSeconds: 1,
	})
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServe(ctx)
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
			return false
		}
		_, isGRPC := tunnel.Conn.(*GRPCManagerTunnelConn)
		return isGRPC
	}, 3*time.Second, 20*time.Millisecond)

	err := <-errCh
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTunnelClient_connectAndServePoll_OpensWebSocketWhenRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var pollCount atomic.Int32
	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			pollCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusRequired,
				PollIntervalSeconds: 1,
			}))
		case "/api/tunnel/connect":
			upgrader := websocket.Upgrader{}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()

			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			registerResp, err := json.Marshal(&TunnelMessage{
				Type:          MessageTypeRegisterResponse,
				Accepted:      true,
				EnvironmentID: "env-poll-ws",
				SessionID:     "session-poll-ws",
			})
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, registerResp); err != nil {
				return
			}

			select {
			case wsConnectedCh <- struct{}{}:
			default:
			}

			<-ctx.Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer managerServer.Close()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	err := client.connectAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected poll transport to open websocket tunnel when required")
	}

	assert.GreaterOrEqual(t, pollCount.Load(), int32(1))
}

func TestTunnelClient_pollTunnelControlInternal_UsesConfiguredHTTPClient(t *testing.T) {
	t.Parallel()

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
			Status:              TunnelStatusIdle,
			PollIntervalSeconds: 1,
		}))
	}))
	defer managerServer.Close()

	baseClient := managerServer.Client()
	rewriteTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(managerServer.URL, "http://")
		return baseClient.Transport.RoundTrip(clone)
	})

	client := NewTunnelClient(&Config{AgentToken: "valid-token"}, http.NotFoundHandler())
	client.httpClient = &http.Client{Transport: rewriteTransport}

	resp, err := client.pollTunnelControlInternal(context.Background(), "http://127.0.0.1:1/api/tunnel/poll", false)
	require.NoError(t, err)
	assert.Equal(t, TunnelStatusIdle, resp.Status)
	assert.Equal(t, 1, resp.PollIntervalSeconds)
	assert.NotNil(t, client.httpClient)
	assert.NotSame(t, http.DefaultClient, client.httpClient)

	defaultReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:1/api/tunnel/poll", nil)
	require.NoError(t, err)
	_, err = http.DefaultClient.Do(defaultReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1")
}

func TestTunnelClient_connectAndServePoll_DoesNotOpenWebSocketWhenIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusIdle,
				PollIntervalSeconds: 1,
			}))
		case "/api/tunnel/connect":
			select {
			case wsConnectedCh <- struct{}{}:
			default:
			}
			http.Error(w, "unexpected websocket connect", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer managerServer.Close()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	err := client.connectAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
		t.Fatal("did not expect idle poll transport to open websocket tunnel")
	default:
	}
}

func TestTunnelClient_connectAndServePoll_RetriesAfterTransientPollError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var pollCount atomic.Int32
	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			currentPoll := pollCount.Add(1)
			if currentPoll == 1 {
				http.Error(w, "temporary failure", http.StatusBadGateway)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusRequired,
				PollIntervalSeconds: 1,
			}))
		case "/api/tunnel/connect":
			upgrader := websocket.Upgrader{}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()

			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			registerResp, err := json.Marshal(&TunnelMessage{
				Type:          MessageTypeRegisterResponse,
				Accepted:      true,
				EnvironmentID: "env-poll-retry-ws",
				SessionID:     "session-poll-retry-ws",
			})
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, registerResp); err != nil {
				return
			}

			select {
			case wsConnectedCh <- struct{}{}:
			default:
			}

			<-ctx.Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer managerServer.Close()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	err := client.connectAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected poll transport to recover after transient poll error")
	}

	assert.GreaterOrEqual(t, pollCount.Load(), int32(2))
}

func TestTunnelClient_stopPollManagedSessionInternal_DeadlineExceededReturnsTimeoutMessage(t *testing.T) {
	t.Parallel()

	session := &pollManagedTunnelSession{
		cancel: func() {},
		done:   make(chan error),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := (&TunnelClient{}).stopPollManagedSessionInternal(ctx, session)
	require.Error(t, err)
	assert.EqualError(t, err, "timed out waiting for poll-managed websocket session to stop")
}

func TestTunnelClient_syncPollManagedSessionInternal_IdleUsesBoundedStopTimeout(t *testing.T) {
	previousTimeout := defaultPollManagedSessionStopTimeout
	defaultPollManagedSessionStopTimeout = 20 * time.Millisecond
	defer func() {
		defaultPollManagedSessionStopTimeout = previousTimeout
	}()

	session := &pollManagedTunnelSession{
		cancel: func() {},
		done:   make(chan error),
	}

	nextSession, err := (&TunnelClient{}).syncPollManagedSessionInternal(context.Background(), session, TunnelStatusIdle)
	require.Nil(t, nextSession)
	require.Error(t, err)
	assert.EqualError(t, err, "timed out waiting for poll-managed websocket session to stop")
}

func startTestPollAndGRPCManagerInternal(t *testing.T, ctx context.Context, service tunnelpb.TunnelServiceServer, pollResp TunnelPollResponse) (string, func()) {
	t.Helper()

	grpcServer := grpc.NewServer()
	tunnelpb.RegisterTunnelServiceServer(grpcServer, service)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tunnel/poll" {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(pollResp))
			return
		}

		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clone := r.Clone(r.Context())
		cloneURL := *clone.URL
		if r.URL.Path == "/api/tunnel/connect" {
			cloneURL.Path = tunnelpb.TunnelService_Connect_FullMethodName
		} else {
			cloneURL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		}
		clone.URL = &cloneURL
		clone.RequestURI = cloneURL.Path
		grpcServer.ServeHTTP(w, clone)
	}), &http2.Server{})

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = httpServer.Serve(lis)
	}()

	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		grpcServer.Stop()
		_ = lis.Close()
	}

	return "http://" + lis.Addr().String(), cleanup
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	if f == nil {
		return nil, fmt.Errorf("round tripper is nil")
	}
	return f(req)
}

func TestTunnelClient_InternalRequestSkipsSlogEcho(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, client *TunnelClient)
	}{
		{
			name: "legacy response",
			run: func(t *testing.T, client *TunnelClient) {
				conn := &capturingTunnelConnForHandleRequest{}
				client.setConn(conn)

				client.handleRequest(context.Background(), conn, &TunnelMessage{
					ID:     "req-legacy",
					Type:   MessageTypeRequest,
					Method: http.MethodGet,
					Path:   "/local/api",
				})

				require.Len(t, conn.sent, 1)
				assert.Equal(t, MessageTypeResponse, conn.sent[0].Type)
				assert.Equal(t, http.StatusOK, conn.sent[0].Status)
				assert.Equal(t, "local response", string(conn.sent[0].Body))
			},
		},
		{
			name: "streaming response",
			run: func(t *testing.T, client *TunnelClient) {
				conn := &fakeTunnelConn{}
				client.setConn(conn)

				client.handleRequestStreaming(context.Background(), conn, &TunnelMessage{
					ID:     "req-stream",
					Type:   MessageTypeRequest,
					Method: http.MethodGet,
					Path:   "/local/api",
				})

				require.Len(t, conn.msgs, 3)
				assert.Equal(t, MessageTypeResponse, conn.msgs[0].Type)
				assert.Equal(t, http.StatusOK, conn.msgs[0].Status)
				assert.Equal(t, MessageTypeStreamData, conn.msgs[1].Type)
				assert.Equal(t, "local response", string(conn.msgs[1].Body))
				assert.Equal(t, MessageTypeStreamEnd, conn.msgs[2].Type)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var sawInternalTunnelRequest bool
			loggerMiddleware := slogecho.New(slog.Default())

			router := echo.New()
			router.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					if IsInternalTunnelRequest(c.Request().Context()) {
						sawInternalTunnelRequest = true
						return next(c)
					}
					return loggerMiddleware(next)(c)
				}
			})
			router.GET("/local/api", func(c echo.Context) error {
				return c.String(http.StatusOK, "local response")
			})

			client := NewTunnelClient(&Config{}, router)
			testCase.run(t, client)

			assert.True(t, sawInternalTunnelRequest)
		})
	}
}

type fakeTunnelConn struct {
	mu      sync.Mutex
	msgs    []*TunnelMessage
	closed  bool
	sendErr error
}

func (f *fakeTunnelConn) Send(msg *TunnelMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	copyMsg := *msg
	if msg.Headers != nil {
		copyMsg.Headers = cloneHeaderMap(msg.Headers)
	}
	if msg.Body != nil {
		copyMsg.Body = append([]byte(nil), msg.Body...)
	}
	f.msgs = append(f.msgs, &copyMsg)
	return nil
}

func (f *fakeTunnelConn) Receive() (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTunnelConn) IsExpectedReceiveError(error) bool {
	return false
}

func (f *fakeTunnelConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeTunnelConn) IsClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeTunnelConn) SendRequest(_ context.Context, _ *TunnelMessage, _ *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func TestStreamingResponseRecorder_Sequence(t *testing.T) {
	conn := &fakeTunnelConn{}
	r := newStreamingResponseRecorder("req-1", conn)

	r.Header().Set("Content-Type", "text/plain")

	n, err := r.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	n, err = r.Write([]byte(" world"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)

	require.NoError(t, r.Close())

	require.Len(t, conn.msgs, 4)
	assert.Equal(t, MessageTypeResponse, conn.msgs[0].Type)
	assert.Equal(t, "req-1", conn.msgs[0].ID)
	assert.Equal(t, "text/plain", conn.msgs[0].Headers["Content-Type"])
	assert.Equal(t, "1", conn.msgs[0].Headers["X-Arcane-Tunnel-Stream"])

	assert.Equal(t, MessageTypeStreamData, conn.msgs[1].Type)
	assert.Equal(t, "hello", string(conn.msgs[1].Body))

	assert.Equal(t, MessageTypeStreamData, conn.msgs[2].Type)
	assert.Equal(t, " world", string(conn.msgs[2].Body))

	assert.Equal(t, MessageTypeStreamEnd, conn.msgs[3].Type)
}

func TestStreamingResponseRecorder_WriteHeaderAndClose(t *testing.T) {
	conn := &fakeTunnelConn{}
	r := newStreamingResponseRecorder("req-2", conn)

	r.WriteHeader(http.StatusCreated)
	require.NoError(t, r.Close())

	require.Len(t, conn.msgs, 2)
	assert.Equal(t, MessageTypeResponse, conn.msgs[0].Type)
	assert.Equal(t, http.StatusCreated, conn.msgs[0].Status)
	assert.Equal(t, MessageTypeStreamEnd, conn.msgs[1].Type)
}
