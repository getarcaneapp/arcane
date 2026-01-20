package edge

import (
	"bytes"
	"context"
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

// setupMockAgentServer creates a WS server that acts as an agent
// It receives requests and sends back responses
func setupMockAgentServer(t *testing.T, handler func(*TunnelMessage) *TunnelMessage) (*httptest.Server, *AgentTunnel) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg TunnelMessage
			_ = json.Unmarshal(data, &msg)

			if msg.Type == MessageTypeRequest {
				resp := handler(&msg)
				respData, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respData)
			}
		}
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}

	tunnel := NewAgentTunnel("env-1", conn)

	// We need a loop to read responses from the tunnel and dispatch them to pending
	go func() {
		for {
			msg, err := tunnel.Conn.Receive()
			if err != nil {
				return
			}
			if req, ok := tunnel.Pending.Load(msg.ID); ok {
				pendingReq := req.(*PendingRequest)
				pendingReq.ResponseCh <- msg
			}
		}
	}()

	return server, tunnel
}

func TestProxyRequest(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusOK,
			Headers: map[string]string{"X-Test": "value"},
			Body:    []byte("response body"),
		}
	})
	defer server.Close()
	defer tunnel.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, headers, body, err := ProxyRequest(ctx, tunnel, "GET", "/api/test", "", nil, nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "value", headers["X-Test"])
	assert.Equal(t, []byte("response body"), body)
}

func TestProxyHTTPRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusCreated,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"success":true}`),
		}
	})
	defer server.Close()
	defer tunnel.Close()

	// Mock Gin context
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("request body"))
	c.Request.Header.Set("X-Custom", "header")

	ProxyHTTPRequest(c, tunnel, "/target/path")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, `{"success":true}`, w.Body.String())
}

func TestDoRequest(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		return &TunnelMessage{
			ID:     msg.ID,
			Type:   MessageTypeResponse,
			Status: http.StatusOK,
			Body:   []byte("ok"),
		}
	})
	defer server.Close()
	defer tunnel.Close()

	// Register tunnel globally
	registry := GetRegistry()
	registry.Register("env-do-req", tunnel)
	defer registry.Unregister("env-do-req")

	ctx := context.Background()
	status, body, err := DoRequest(ctx, "env-do-req", "GET", "/path", nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, []byte("ok"), body)
}

func TestDoRequest_NoTunnel(t *testing.T) {
	_, _, err := DoRequest(context.Background(), "non-existent", "GET", "/", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active tunnel")
}

func TestHasActiveTunnel(t *testing.T) {
	conn := createTestConn(t)
	defer conn.Close()
	tunnel := NewAgentTunnel("env-active", conn)

	registry := GetRegistry()
	registry.Register("env-active", tunnel)
	defer registry.Unregister("env-active")

	assert.True(t, HasActiveTunnel("env-active"))
	assert.False(t, HasActiveTunnel("non-existent"))

	tunnel.Close()
	assert.False(t, HasActiveTunnel("env-active"))
}

func TestIsHopByHopHeader(t *testing.T) {
	assert.True(t, isHopByHopHeader("Connection"))
	assert.True(t, isHopByHopHeader("Keep-Alive"))
	assert.True(t, isHopByHopHeader("Proxy-Authenticate"))
	assert.True(t, isHopByHopHeader("Upgrade"))
	assert.False(t, isHopByHopHeader("Content-Type"))
	assert.False(t, isHopByHopHeader("X-Custom-Header"))
}
