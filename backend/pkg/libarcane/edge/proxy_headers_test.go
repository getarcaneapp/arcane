package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBrowserSecurityHeader(t *testing.T) {
	browserHeaders := []string{
		"Origin",
		"Referer",
		"Cookie",
		"Access-Control-Request-Method",
		"Access-Control-Request-Headers",
		"Sec-Fetch-Mode",
		"Sec-Fetch-Site",
		"Sec-Fetch-Dest",
	}

	for _, h := range browserHeaders {
		assert.True(t, isBrowserSecurityHeader(h), "expected %q to be a browser security header", h)
	}

	nonBrowserHeaders := []string{
		"Content-Type",
		"Authorization",
		"X-Custom-Header",
		"X-Arcane-Agent-Token",
		"X-API-Key",
		"Accept",
	}

	for _, h := range nonBrowserHeaders {
		assert.False(t, isBrowserSecurityHeader(h), "expected %q to NOT be a browser security header", h)
	}
}

func TestProxyHTTPRequest_StripsBrowserHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Track what headers the agent receives
	var receivedHeaders map[string]string

	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		receivedHeaders = msg.Headers
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusOK,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"ok":true}`),
		}
	})
	defer server.Close()
	defer func() { _ = tunnel.Close() }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"name":"test"}`))

	// Set headers that browsers add
	c.Request.Header.Set("Origin", "http://192.168.1.42:30258")
	c.Request.Header.Set("Referer", "http://192.168.1.42:30258/projects/new")
	c.Request.Header.Set("Cookie", "token=some-jwt-token")
	c.Request.Header.Set("Sec-Fetch-Mode", "cors")
	c.Request.Header.Set("Sec-Fetch-Site", "same-origin")
	c.Request.Header.Set("Sec-Fetch-Dest", "empty")
	c.Request.Header.Set("Access-Control-Request-Method", "POST")
	c.Request.Header.Set("Access-Control-Request-Headers", "Content-Type")

	// Set headers that SHOULD be forwarded
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Arcane-Agent-Token", "agent-tok-123")
	c.Request.Header.Set("X-API-Key", "agent-tok-123")
	c.Request.Header.Set("Authorization", "Bearer jwt-token")

	ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.Equal(t, http.StatusOK, w.Code)

	// Browser security headers should NOT be present on the agent side
	assert.Empty(t, receivedHeaders["Origin"], "Origin header should be stripped")
	assert.Empty(t, receivedHeaders["Referer"], "Referer header should be stripped")
	assert.Empty(t, receivedHeaders["Cookie"], "Cookie header should be stripped")
	assert.Empty(t, receivedHeaders["Sec-Fetch-Mode"], "Sec-Fetch-Mode header should be stripped")
	assert.Empty(t, receivedHeaders["Sec-Fetch-Site"], "Sec-Fetch-Site header should be stripped")
	assert.Empty(t, receivedHeaders["Sec-Fetch-Dest"], "Sec-Fetch-Dest header should be stripped")
	assert.Empty(t, receivedHeaders["Access-Control-Request-Method"], "Access-Control-Request-Method should be stripped")
	assert.Empty(t, receivedHeaders["Access-Control-Request-Headers"], "Access-Control-Request-Headers should be stripped")

	// Important headers SHOULD still be forwarded
	assert.Equal(t, "application/json", receivedHeaders["Content-Type"])
	assert.Equal(t, "agent-tok-123", receivedHeaders["X-Arcane-Agent-Token"])
	assert.Equal(t, "agent-tok-123", receivedHeaders["X-Api-Key"])
	assert.Equal(t, "Bearer jwt-token", receivedHeaders["Authorization"])
}

func TestProxyHTTPRequest_ForwardsBodyCorrectly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var receivedBody []byte
	var receivedMethod string

	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		receivedBody = msg.Body
		receivedMethod = msg.Method
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusCreated,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"id":"proj-1","name":"test"}`),
		}
	})
	defer server.Close()
	defer func() { _ = tunnel.Close() }()

	requestBody := `{"name":"My Project"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/environments/abc/projects", bytes.NewBufferString(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Arcane-Agent-Token", "tok")

	ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Equal(t, requestBody, string(receivedBody), "Request body should be forwarded intact through the tunnel")
}

func TestHandleRequest_SetsHostField(t *testing.T) {
	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Host is properly set from the tunneled headers
		assert.Equal(t, "my-agent-host:3552", r.Host, "Host field should be set from tunnel message headers")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	client := NewTunnelClient(&config.Config{}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.conn = conn

	client.handleRequest(context.Background(), &TunnelMessage{
		ID:     "req-host-1",
		Type:   MessageTypeRequest,
		Method: http.MethodGet,
		Path:   "/api/test",
		Headers: map[string]string{
			"Host":                 "my-agent-host:3552",
			"X-Arcane-Agent-Token": "tok-123",
			"Content-Type":         "application/json",
		},
	})

	require.Len(t, conn.sent, 1)
	assert.Equal(t, http.StatusOK, conn.sent[0].Status)
}

func TestHandleRequest_ForwardsBody(t *testing.T) {
	requestBody := `{"name":"My Project"}`
	var receivedBody string

	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p1"}`))
	})

	client := NewTunnelClient(&config.Config{}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.conn = conn

	client.handleRequest(context.Background(), &TunnelMessage{
		ID:     "req-body-1",
		Type:   MessageTypeRequest,
		Method: http.MethodPost,
		Path:   "/api/environments/0/projects",
		Headers: map[string]string{
			"Content-Type":         "application/json",
			"X-Arcane-Agent-Token": "tok-123",
		},
		Body: []byte(requestBody),
	})

	require.Len(t, conn.sent, 1)
	assert.Equal(t, http.StatusCreated, conn.sent[0].Status)
	assert.Equal(t, requestBody, receivedBody, "Request body should be forwarded to the local handler")
}

func TestHandleRequest_NoBrowserHeadersInTunnelMessage(t *testing.T) {
	// Verify that if somehow browser headers leaked into a tunnel message,
	// they are still forwarded as-is (stripping happens on the proxy side, not the client side).
	// This test documents that the client sets all received headers.
	var receivedOrigin string

	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedOrigin = r.Header.Get("Origin")
		w.WriteHeader(http.StatusOK)
	})

	client := NewTunnelClient(&config.Config{}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.conn = conn

	// Simulate an old manager that doesn't strip Origin
	client.handleRequest(context.Background(), &TunnelMessage{
		ID:     "req-origin-1",
		Type:   MessageTypeRequest,
		Method: http.MethodPost,
		Path:   "/test",
		Headers: map[string]string{
			"Origin": "http://bad-origin:1234",
		},
	})

	// The client doesn't strip - it passes whatever it gets to the handler
	assert.Equal(t, "http://bad-origin:1234", receivedOrigin,
		"Client should forward all headers; stripping is the proxy's responsibility")
}

func TestProxyHTTPRequest_EndToEnd_BrowserOriginStripped(t *testing.T) {
	// Full end-to-end test: browser sends POST with Origin header via edge tunnel.
	// The agent should NOT receive the Origin header.
	gin.SetMode(gin.TestMode)

	// This handler simulates what the agent's Gin handler would do.
	// In a real scenario, if Origin is present and doesn't match allowed origins,
	// gin-contrib/cors would return 403 before this handler runs.
	agentHandlerCalled := false
	agentHandler := func(msg *TunnelMessage) *TunnelMessage {
		agentHandlerCalled = true
		// If Origin is present, that would cause a 403 in real life
		if msg.Headers["Origin"] != "" {
			return &TunnelMessage{
				ID:     msg.ID,
				Type:   MessageTypeResponse,
				Status: http.StatusForbidden,
				Body:   []byte("Origin header should have been stripped"),
			}
		}
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusCreated,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"id":"project-1"}`),
		}
	}

	server, tunnel := setupMockAgentServer(t, agentHandler)
	defer server.Close()
	defer func() { _ = tunnel.Close() }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	body := `{"name":"New Project"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/environments/env-123/projects", bytes.NewBufferString(body))
	c.Request.Header.Set("Origin", "http://192.168.1.42:30258")
	c.Request.Header.Set("Referer", "http://192.168.1.42:30258/projects/new")
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Arcane-Agent-Token", "agent-tok")

	ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.True(t, agentHandlerCalled, "Agent handler should have been called")
	assert.Equal(t, http.StatusCreated, w.Code, "Request should succeed because Origin was stripped")
	assert.Contains(t, w.Body.String(), "project-1")
}

func TestProxyHTTPRequest_BodyPreservation_WebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test that a POST body survives WebSocket JSON serialization round-trip
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg TunnelMessage
			_ = json.Unmarshal(data, &msg)

			if msg.Type == MessageTypeRequest {
				receivedBody = string(msg.Body)
				resp := &TunnelMessage{
					ID:      msg.ID,
					Type:    MessageTypeResponse,
					Status:  http.StatusCreated,
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    []byte(`{"ok":true}`),
				}
				respData, _ := json.Marshal(resp)
				_ = conn.WriteMessage(websocket.TextMessage, respData)
			}
		}
	}))
	defer server.Close()

	url := "ws" + server.URL[4:]
	wsConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	tunnel := NewAgentTunnel("env-body-test", wsConn)
	defer func() { _ = tunnel.Close() }()

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

	requestBody := `{"name":"My Project","description":"A test project with special chars: <>&\""}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")

	ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, requestBody, receivedBody, "Request body should survive WebSocket JSON serialization round-trip")
}
