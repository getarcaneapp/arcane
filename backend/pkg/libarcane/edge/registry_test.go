package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestConn(t *testing.T) *websocket.Conn {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	return conn
}

func TestTunnelRegistry(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	// Create a tunnel
	conn := createTestConn(t)
	defer func() { _ = conn.Close() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)

	// Register
	r.Register(envID, tunnel)

	// Get
	got, ok := r.Get(envID)
	assert.True(t, ok)
	assert.Equal(t, tunnel, got)

	// Unregister
	r.Unregister(envID)
	_, ok = r.Get(envID)
	assert.False(t, ok)

	// Test Connection Closed after Unregister
	assert.True(t, tunnel.Conn.IsClosed())
}

func TestTunnelRegistry_RegisterReplace(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.Close() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	r.Register(envID, tunnel1)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.Close() }()
	tunnel2 := newWebSocketAgentTunnel(envID, conn2)

	// Register replacement
	r.Register(envID, tunnel2)

	// Check replacement
	got, ok := r.Get(envID)
	assert.True(t, ok)
	assert.Equal(t, tunnel2, got)

	// First tunnel should be closed
	assert.True(t, tunnel1.Conn.IsClosed())
	assert.False(t, tunnel2.Conn.IsClosed())
}

func TestTunnelRegistry_RegisterSessionRejectsCompetingAgent(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-session-reject"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.Close() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	tunnel1.AgentInstance = "agent-a"
	tunnel1.SessionID = "session-a"

	accepted, drainPrevious, reason := r.RegisterSession(tunnel1, TunnelStaleTimeout)
	assert.True(t, accepted)
	assert.False(t, drainPrevious)
	assert.Equal(t, "", reason)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.Close() }()
	tunnel2 := newWebSocketAgentTunnel(envID, conn2)
	tunnel2.AgentInstance = "agent-b"
	tunnel2.SessionID = "session-b"

	accepted, drainPrevious, reason = r.RegisterSession(tunnel2, TunnelStaleTimeout)
	assert.False(t, accepted)
	assert.False(t, drainPrevious)
	assert.Equal(t, "another edge agent session is already active", reason)

	got, ok := r.Get(envID)
	assert.True(t, ok)
	assert.Equal(t, tunnel1, got)
	assert.False(t, tunnel1.Conn.IsClosed())
}

func TestTunnelRegistry_RegisterSessionReplacesSameAgentInstance(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-session-replace"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.Close() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	tunnel1.AgentInstance = "agent-a"
	tunnel1.SessionID = "session-a"
	accepted, drainPrevious, reason := r.RegisterSession(tunnel1, TunnelStaleTimeout)
	assert.True(t, accepted)
	assert.False(t, drainPrevious)
	assert.Equal(t, "", reason)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.Close() }()
	tunnel2 := newWebSocketAgentTunnel(envID, conn2)
	tunnel2.AgentInstance = "agent-a"
	tunnel2.SessionID = "session-b"
	accepted, drainPrevious, reason = r.RegisterSession(tunnel2, TunnelStaleTimeout)
	assert.True(t, accepted)
	assert.True(t, drainPrevious)
	assert.Equal(t, "", reason)
	assert.True(t, tunnel1.Conn.IsClosed())

	got, ok := r.Get(envID)
	assert.True(t, ok)
	assert.Equal(t, tunnel2, got)
}

func TestTunnelRegistry_CleanupStale(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	conn := createTestConn(t)
	defer func() { _ = conn.Close() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)

	// Manually set last heartbeat to past
	tunnel.mu.Lock()
	tunnel.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	tunnel.mu.Unlock()

	r.Register(envID, tunnel)

	// Cleanup
	removed := r.CleanupStale(5 * time.Minute)
	assert.Equal(t, 1, removed)

	_, ok := r.Get(envID)
	assert.False(t, ok)
	assert.True(t, tunnel.Conn.IsClosed())
}

func TestGetRegistry(t *testing.T) {
	r1 := GetRegistry()
	r2 := GetRegistry()
	assert.Equal(t, r1, r2)
}

func TestAgentTunnel_Heartbeat(t *testing.T) {
	conn := createTestConn(t)
	defer func() { _ = conn.Close() }()
	tunnel := newWebSocketAgentTunnel("env-1", conn)

	initial := tunnel.GetLastHeartbeat()
	time.Sleep(10 * time.Millisecond)

	tunnel.UpdateHeartbeat()
	updated := tunnel.GetLastHeartbeat()

	assert.True(t, updated.After(initial))
}
