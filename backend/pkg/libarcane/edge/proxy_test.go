package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
		defer func() { _ = conn.Close() }()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg TunnelMessage
			_ = json.Unmarshal(data, &msg)

			if msg.Type == MessageTypeRequest || msg.Type == MessageTypeCommandRequest {
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
		defer func() { _ = resp.Body.Close() }()
	}

	tunnel := newWebSocketAgentTunnel("env-1", conn)

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
	defer func() { _ = tunnel.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, headers, body, err := ProxyRequest(ctx, tunnel, http.MethodGet, "/api/health", "", nil, nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "value", headers["X-Test"])
	assert.Equal(t, []byte("response body"), body)
}

func TestProxyHTTPRequest(t *testing.T) {
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
	defer func() { _ = tunnel.Close() }()

	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("request body"))
	req.Header.Set("X-Custom", "header")
	c := e.NewContext(req, w)

	_ = ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, `{"success":true}`, w.Body.String())
}

func TestProxyHTTPRequest_GRPCTunnel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-proxy-http-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())

	agentErrCh := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			agentErrCh <- err
			return
		}

		req := msg.GetCommandRequest()
		if req == nil {
			agentErrCh <- errors.New("expected command request")
			return
		}

		if req.GetMethod() != http.MethodPost {
			agentErrCh <- errors.New("unexpected method")
			return
		}
		if req.GetPath() != "/api/environments/0/projects" {
			agentErrCh <- errors.New("unexpected path")
			return
		}
		if req.GetQuery() != "from=test" {
			agentErrCh <- errors.New("unexpected query")
			return
		}
		if req.GetHeaders()["X-Custom"] != "header" {
			agentErrCh <- errors.New("missing X-Custom header")
			return
		}
		if req.GetHeaders()["Connection"] != "" {
			agentErrCh <- errors.New("connection header should not be forwarded")
			return
		}
		if string(req.GetBody()) != "request body" {
			agentErrCh <- errors.New("unexpected request body")
			return
		}

		if err := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandAck{CommandAck: &tunnelpb.CommandAck{
			CommandId: req.GetCommandId(),
		}}}); err != nil {
			agentErrCh <- err
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandComplete{CommandComplete: &tunnelpb.CommandComplete{
			CommandId: req.GetCommandId(),
			Status:    http.StatusCreated,
			Headers:   map[string]string{"Content-Type": "application/json"},
			Body:      []byte(`{"success":true}`),
		}}})
	}()

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, time.Second, 10*time.Millisecond)

	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test?from=test", bytes.NewBufferString("request body"))
	req.Header.Set("X-Custom", "header")
	req.Header.Set("Connection", "keep-alive")
	c := e.NewContext(req, w)

	_ = ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, `{"success":true}`, w.Body.String())

	require.NoError(t, <-agentErrCh)
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
	defer func() { _ = tunnel.Close() }()

	// Register tunnel globally
	registry := GetRegistry()
	registry.Register("env-do-req", tunnel)
	defer registry.Unregister("env-do-req")

	ctx := context.Background()
	status, body, err := DoRequest(ctx, "env-do-req", "GET", "/api/health", nil)

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
	defer func() { _ = conn.Close() }()
	tunnel := newWebSocketAgentTunnel("env-active", conn)

	registry := GetRegistry()
	registry.Register("env-active", tunnel)
	defer registry.Unregister("env-active")

	assert.True(t, HasActiveTunnel("env-active"))
	assert.False(t, HasActiveTunnel("non-existent"))

	_ = tunnel.Close()
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

	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"name":"test"}`))

	req.Header.Set("Origin", "http://192.168.1.42:30258")
	req.Header.Set("Referer", "http://192.168.1.42:30258/projects/new")
	req.Header.Set("Cookie", "token=some-jwt-token")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arcane-Agent-Token", "agent-tok-123")
	req.Header.Set("X-API-Key", "agent-tok-123")
	req.Header.Set("Authorization", "Bearer jwt-token")
	c := e.NewContext(req, w)

	_ = ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

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

	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/environments/abc/projects", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arcane-Agent-Token", "tok")
	c := e.NewContext(req, w)

	_ = ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

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

	client := NewTunnelClient(&Config{}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.setConn(conn)

	client.handleRequest(context.Background(), conn, &TunnelMessage{
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

	client := NewTunnelClient(&Config{}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.setConn(conn)

	client.handleRequest(context.Background(), conn, &TunnelMessage{
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

	client := NewTunnelClient(&Config{}, localHandler)
	conn := &capturingTunnelConnForHandleRequest{}
	client.setConn(conn)

	// Simulate an old manager that doesn't strip Origin
	client.handleRequest(context.Background(), conn, &TunnelMessage{
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

	// This handler simulates what the agent's handler would do.
	// In a real scenario, if Origin is present and doesn't match allowed origins,
	// CORS middleware would return 403 before this handler runs.
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

	e := echo.New()
	w := httptest.NewRecorder()
	body := `{"name":"New Project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-123/projects", bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://192.168.1.42:30258")
	req.Header.Set("Referer", "http://192.168.1.42:30258/projects/new")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arcane-Agent-Token", "agent-tok")
	c := e.NewContext(req, w)

	_ = ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.True(t, agentHandlerCalled, "Agent handler should have been called")
	assert.Equal(t, http.StatusCreated, w.Code, "Request should succeed because Origin was stripped")
	assert.Contains(t, w.Body.String(), "project-1")
}

func TestProxyHTTPRequest_BodyPreservation_WebSocket(t *testing.T) {
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

			if msg.Type == MessageTypeRequest || msg.Type == MessageTypeCommandRequest {
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

	tunnel := newWebSocketAgentTunnel("env-body-test", wsConn)
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

	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/json")
	c := e.NewContext(req, w)

	_ = ProxyHTTPRequest(c, tunnel, "/api/environments/0/projects")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, requestBody, receivedBody, "Request body should survive WebSocket JSON serialization round-trip")
}
