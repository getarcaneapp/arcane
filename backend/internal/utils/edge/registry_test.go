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
		defer resp.Body.Close()
	}
	return conn
}

func TestTunnelRegistry(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	// Create a tunnel
	conn := createTestConn(t)
	defer conn.Close()
	tunnel := NewAgentTunnel(envID, conn)

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
	defer conn1.Close()
	tunnel1 := NewAgentTunnel(envID, conn1)
	r.Register(envID, tunnel1)

	conn2 := createTestConn(t)
	defer conn2.Close()
	tunnel2 := NewAgentTunnel(envID, conn2)

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

func TestTunnelRegistry_CleanupStale(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	conn := createTestConn(t)
	defer conn.Close()
	tunnel := NewAgentTunnel(envID, conn)

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
	defer conn.Close()
	tunnel := NewAgentTunnel("env-1", conn)

	initial := tunnel.GetLastHeartbeat()
	time.Sleep(10 * time.Millisecond)

	tunnel.UpdateHeartbeat()
	updated := tunnel.GetLastHeartbeat()

	assert.True(t, updated.After(initial))
}
