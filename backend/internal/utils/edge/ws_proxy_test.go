package edge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyWebSocketRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Custom Mock Agent Server
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg TunnelMessage
			_ = json.Unmarshal(data, &msg)

			if msg.Type == MessageTypeWebSocketStart {
				// 1. Send Data
				resp := &TunnelMessage{
					ID:            msg.ID,
					Type:          MessageTypeWebSocketData,
					Body:          []byte("hello"),
					WSMessageType: websocket.TextMessage,
				}
				respData, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respData)

				// 2. Send Unknown Type (should be ignored by proxy)
				unknown := &TunnelMessage{
					ID:   msg.ID,
					Type: "unknown_proxy_type",
				}
				unknownData, _ := json.Marshal(unknown)
				_ = conn.WriteMessage(websocket.TextMessage, unknownData)

				// 3. Send Close
				closeMsg := &TunnelMessage{
					ID:   msg.ID,
					Type: MessageTypeWebSocketClose,
				}
				closeData, _ := json.Marshal(closeMsg)
				_ = conn.WriteMessage(websocket.TextMessage, closeData)
			}
		}
	}))
	defer agentServer.Close()

	// Connect Agent Tunnel
	url := "ws" + strings.TrimPrefix(agentServer.URL, "http")
	agentConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}

	tunnel := NewAgentTunnel("env-ws-proxy", agentConn)
	defer tunnel.Close()

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
	ginRouter := gin.New()
	ginRouter.GET("/proxy-ws", func(c *gin.Context) {
		ProxyWebSocketRequest(c, tunnel, "/target/ws")
	})

	proxyServer := httptest.NewServer(ginRouter)
	defer proxyServer.Close()

	// Client Connect
	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/proxy-ws"
	clientConn, clientResp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if clientResp != nil {
		defer clientResp.Body.Close()
	}
	defer clientConn.Close()

	// Read Hello
	_, msg, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello", string(msg))

	// Send data from Client to Agent
	err = clientConn.WriteMessage(websocket.TextMessage, []byte("client-data"))
	require.NoError(t, err)

	// Give a bit of time for the forward to happen
	time.Sleep(50 * time.Millisecond)

	// So client should see connection close.
	_, _, err = clientConn.ReadMessage()
	// Should be EOF or close error
	assert.Error(t, err)
}

func TestProxyWebSocketRequest_ClientClose(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup simple agent that just reads
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer agentServer.Close()

	url := "ws" + strings.TrimPrefix(agentServer.URL, "http")
	agentConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}

	tunnel := NewAgentTunnel("env-ws-close", agentConn)
	defer tunnel.Close()

	go func() {
		for {
			if _, err := tunnel.Conn.Receive(); err != nil {
				return
			}
		}
	}()

	ginRouter := gin.New()
	ginRouter.GET("/proxy-ws", func(c *gin.Context) {
		ProxyWebSocketRequest(c, tunnel, "/target/ws")
	})
	proxyServer := httptest.NewServer(ginRouter)
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/proxy-ws"
	clientConn, clientResp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if clientResp != nil {
		defer clientResp.Body.Close()
	}

	// Client closes
	clientConn.Close()

	// Server side should handle it gracefully
	time.Sleep(100 * time.Millisecond)
}

func TestIsForwardableWSMessage(t *testing.T) {
	assert.True(t, isForwardableWSMessage(websocket.TextMessage))
	assert.True(t, isForwardableWSMessage(websocket.BinaryMessage))
	assert.False(t, isForwardableWSMessage(websocket.PingMessage))
	assert.False(t, isForwardableWSMessage(websocket.PongMessage))
	assert.False(t, isForwardableWSMessage(websocket.CloseMessage))
}

func TestSendWebSocketData(t *testing.T) {
	// Mock Agent Tunnel
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// Read message
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg TunnelMessage
		_ = json.Unmarshal(data, &msg)

		assert.Equal(t, MessageTypeWebSocketData, msg.Type)
		assert.Equal(t, "test-stream", msg.ID)
		assert.Equal(t, websocket.TextMessage, msg.WSMessageType)
		assert.Equal(t, "payload", string(msg.Body))
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	tunnel := NewAgentTunnel("env-helper", conn)
	defer tunnel.Close()

	err = sendWebSocketData(tunnel, "test-stream", websocket.TextMessage, []byte("payload"))
	require.NoError(t, err)
}

func TestSendWebSocketClose(t *testing.T) {
	// Mock Agent Tunnel
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// Read message
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg TunnelMessage
		_ = json.Unmarshal(data, &msg)

		assert.Equal(t, MessageTypeWebSocketClose, msg.Type)
		assert.Equal(t, "test-stream", msg.ID)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}

	tunnel := NewAgentTunnel("env-helper-close", conn)
	defer tunnel.Close()

	sendWebSocketClose(tunnel, "test-stream")
}
