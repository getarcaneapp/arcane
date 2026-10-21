package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMockAgentServer creates a WS server that acts as an agent serving the multiplexed data plane with handler.
func setupMockAgentServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *AgentTunnel) {
	agent := &TunnelClient{handler: handler}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = agent.serveMuxSession(r.Context(), NewTunnelConn(conn).muxConn(r.Context()))
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	tunnel := newWebSocketAgentTunnel("env-1", conn)
	go func() {
		_ = (&TunnelServer{}).serveMuxTunnel(t.Context(), tunnel, tunnel.Conn.muxConn(t.Context()))
	}()

	return server, tunnel
}

func TestProxyRequest(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test", "value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response body"))
	})
	defer server.Close()
	defer func() { _ = tunnel.CloseWithReason("") }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	status, headers, body, err := ProxyRequest(ctx, tunnel, http.MethodGet, "/api/health", "", nil, nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "value", headers["X-Test"])
	assert.Equal(t, []byte("response body"), body)
}

func TestDoRequest(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	defer server.Close()
	defer func() { _ = tunnel.CloseWithReason("") }()

	// Register tunnel globally
	registry := GetRegistry()
	registry.Register("env-do-req", tunnel)
	defer registry.Unregister("env-do-req")

	ctx := t.Context()
	status, body, err := DoRequest(ctx, "env-do-req", "GET", "/api/health", nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, []byte("ok"), body)
}

func TestDoRequest_NoTunnel(t *testing.T) {
	_, _, err := DoRequest(t.Context(), "non-existent", "GET", "/", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active tunnel")
}

func TestHasActiveTunnel(t *testing.T) {
	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel("env-active", conn)

	registry := GetRegistry()
	registry.Register("env-active", tunnel)
	defer registry.Unregister("env-active")

	assert.True(t, HasActiveTunnel("env-active"))
	assert.False(t, HasActiveTunnel("non-existent"))

	_ = tunnel.CloseWithReason("")
	assert.False(t, HasActiveTunnel("env-active"))
}
