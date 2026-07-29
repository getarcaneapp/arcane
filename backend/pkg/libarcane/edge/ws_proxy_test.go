package edge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyWebSocketRequest(t *testing.T) {
	// Custom Mock Agent Server
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}

			var msg TunnelMessage
			_ = json.Unmarshal(data, &msg)

			if msg.Type == MessageTypeStreamOpen {
				// 1. Send Data
				resp := &TunnelMessage{
					ID:            msg.ID,
					Type:          MessageTypeStreamData,
					Body:          []byte("hello"),
					WSMessageType: int(websocket.MessageText),
				}
				respData, _ := json.Marshal(resp)
				_ = conn.Write(r.Context(), websocket.MessageText, respData)

				// 2. Send Unknown Type (should be ignored by proxy)
				unknown := &TunnelMessage{
					ID:   msg.ID,
					Type: "unknown_proxy_type",
				}
				unknownData, _ := json.Marshal(unknown)
				_ = conn.Write(r.Context(), websocket.MessageText, unknownData)

				// 3. Send Close
				closeMsg := &TunnelMessage{
					ID:   msg.ID,
					Type: MessageTypeStreamClose,
				}
				closeData, _ := json.Marshal(closeMsg)
				_ = conn.Write(r.Context(), websocket.MessageText, closeData)
			}
		}
	}))
	defer agentServer.Close()

	// Connect Agent Tunnel
	url := "ws" + strings.TrimPrefix(agentServer.URL, "http")
	agentConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	tunnel := newWebSocketAgentTunnel("env-ws-proxy", agentConn)
	defer func() { _ = tunnel.CloseWithReason("") }()

	// Start receiving on tunnel
	go func() {
		for {
			msg, err := tunnel.Conn.Receive()
			if err != nil {
				return
			}
			if req, ok := tunnel.Pending.Load(msg.ID); ok {
				pendingReq := req.(*PendingRequest)
				// Non-blocking send
				select {
				case pendingReq.ResponseCh <- msg:
				default:
				}
			}
		}
	}()

	// Setup Manager
	router := echo.New()
	router.GET("/proxy-ws", func(c *echo.Context) error {
		return ProxyWebSocketRequest(c, tunnel, "/api/environments/0/ws/system/stats", func(*http.Request) bool { return true })
	})

	proxyServer := httptest.NewServer(router)
	defer proxyServer.Close()

	// Client Connect
	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/proxy-ws"
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	// Read Hello
	_, msg, err := clientConn.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "hello", string(msg))

	// Send data from Client to Agent
	err = clientConn.Write(t.Context(), websocket.MessageText, []byte("client-data"))
	require.NoError(t, err)

	// Give a bit of time for the forward to happen
	time.Sleep(50 * time.Millisecond)

	// So client should see connection close.
	_, _, err = clientConn.Read(t.Context())
	// Should be EOF or close error
	assert.Error(t, err)
}

func TestProxyWebSocketRequest_ClientClose(t *testing.T) {
	// Setup simple agent that just reads
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer agentServer.Close()

	url := "ws" + strings.TrimPrefix(agentServer.URL, "http")
	agentConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	tunnel := newWebSocketAgentTunnel("env-ws-close", agentConn)
	defer func() { _ = tunnel.CloseWithReason("") }()

	go func() {
		for {
			if _, err := tunnel.Conn.Receive(); err != nil {
				return
			}
		}
	}()

	router := echo.New()
	router.GET("/proxy-ws", func(c *echo.Context) error {
		return ProxyWebSocketRequest(c, tunnel, "/api/environments/0/ws/system/stats", func(*http.Request) bool { return true })
	})
	proxyServer := httptest.NewServer(router)
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/proxy-ws"
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)

	// Client closes
	_ = clientConn.CloseNow()

	// Server side should handle it gracefully
	time.Sleep(100 * time.Millisecond)
}

func TestIsForwardableWSMessage(t *testing.T) {
	assert.True(t, isForwardableWSMessage(int(websocket.MessageText)))
	assert.True(t, isForwardableWSMessage(int(websocket.MessageBinary)))
	// coder/websocket does not export control-frame message types; use the
	// raw RFC 6455 opcodes (close=8, ping=9, pong=10).
	assert.False(t, isForwardableWSMessage(9))
	assert.False(t, isForwardableWSMessage(10))
	assert.False(t, isForwardableWSMessage(8))
}

func TestSendWebSocketData(t *testing.T) {
	// Mock Agent Tunnel
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		// Read message
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var msg TunnelMessage
		_ = json.Unmarshal(data, &msg)

		assert.Equal(t, MessageTypeStreamData, msg.Type)
		assert.Equal(t, "test-stream", msg.ID)
		assert.Equal(t, int(websocket.MessageText), msg.WSMessageType)
		assert.Equal(t, "payload", string(msg.Body))

		// Drain until the peer closes so the close handshake completes promptly.
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	tunnel := newWebSocketAgentTunnel("env-helper", conn)
	defer func() { _ = tunnel.CloseWithReason("") }()

	err = sendStreamDataInternal(tunnel, "test-stream", int(websocket.MessageText), []byte("payload"))
	require.NoError(t, err)
}

func TestSendWebSocketClose(t *testing.T) {
	// Mock Agent Tunnel
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		// Read message
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var msg TunnelMessage
		_ = json.Unmarshal(data, &msg)

		assert.Equal(t, MessageTypeStreamClose, msg.Type)
		assert.Equal(t, "test-stream", msg.ID)

		// Drain until the peer closes so the close handshake completes promptly.
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	tunnel := newWebSocketAgentTunnel("env-helper-close", conn)
	defer func() { _ = tunnel.CloseWithReason("") }()

	sendWebSocketClose(tunnel, "test-stream")
}

func TestHandleAgentMessage_StreamDataPreservesTextFrame(t *testing.T) {
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		serverConnCh <- conn
	}))
	defer server.Close()

	clientURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), clientURL, nil)
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	serverConn := <-serverConnCh
	defer func() { _ = serverConn.CloseNow() }()

	stop, err := handleAgentMessage(t.Context(), t.Context(), serverConn, &TunnelMessage{
		ID:            "stats-stream",
		Type:          MessageTypeStreamData,
		Body:          []byte(`{"cpuPercent":12.5}`),
		WSMessageType: int(websocket.MessageText),
	}, "stats-stream")
	require.NoError(t, err)
	assert.False(t, stop)

	msgType, payload, err := clientConn.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, msgType)
	assert.Equal(t, `{"cpuPercent":12.5}`, string(payload))
}
