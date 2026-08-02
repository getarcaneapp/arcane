package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type controlledCloseTunnelConn struct {
	closeStarted chan struct{}
	releaseClose chan struct{}
	closed       chan struct{}
}

func (c *controlledCloseTunnelConn) Send(*TunnelMessage) error         { return nil }
func (c *controlledCloseTunnelConn) Receive() (*TunnelMessage, error)  { return nil, nil }
func (c *controlledCloseTunnelConn) IsExpectedReceiveError(error) bool { return false }
func (c *controlledCloseTunnelConn) Transport() string                 { return EdgeTransportWebSocket }
func (c *controlledCloseTunnelConn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
func (c *controlledCloseTunnelConn) Close() error {
	select {
	case <-c.closeStarted:
	default:
		close(c.closeStarted)
	}
	<-c.releaseClose
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func createTestConn(t *testing.T) *websocket.Conn {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Block until the peer closes so close handshakes complete promptly.
		_, _, _ = conn.Read(r.Context())
	}))
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	return conn
}

func TestTunnelRegistry(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	// Create a tunnel
	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)

	// Register
	r.Register(envID, tunnel)

	// Get
	got, ok := r.Get(envID).Get()
	assert.True(t, ok)
	assert.Equal(t, tunnel, got)

	// Unregister
	r.Unregister(envID)
	_, ok = r.Get(envID).Get()
	assert.False(t, ok)

	// Test Connection Closed after Unregister
	assert.True(t, tunnel.Conn.IsClosed())
}

func TestTunnelRegistry_RegisterReplace(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.CloseNow() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	r.Register(envID, tunnel1)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.CloseNow() }()
	tunnel2 := newWebSocketAgentTunnel(envID, conn2)

	// Register replacement
	r.Register(envID, tunnel2)

	// Check replacement
	got, ok := r.Get(envID).Get()
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
	defer func() { _ = conn1.CloseNow() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	tunnel1.AgentInstance = "agent-a"
	tunnel1.SessionID = "session-a"

	accepted, drainPrevious, reason, err := r.RegisterSession(t.Context(), tunnel1, TunnelStaleTimeout)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.False(t, drainPrevious)
	assert.Empty(t, reason)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.CloseNow() }()
	tunnel2 := newWebSocketAgentTunnel(envID, conn2)
	tunnel2.AgentInstance = "agent-b"
	tunnel2.SessionID = "session-b"

	accepted, drainPrevious, reason, err = r.RegisterSession(t.Context(), tunnel2, TunnelStaleTimeout)
	require.NoError(t, err)
	assert.False(t, accepted)
	assert.False(t, drainPrevious)
	assert.Equal(t, "another edge agent session is already active", reason)

	got, ok := r.Get(envID).Get()
	assert.True(t, ok)
	assert.Equal(t, tunnel1, got)
	assert.False(t, tunnel1.Conn.IsClosed())
}

func TestTunnelRegistry_RegisterSessionReplacesSameAgentInstance(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-session-replace"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.CloseNow() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	tunnel1.AgentInstance = "agent-a"
	tunnel1.SessionID = "session-a"
	accepted, drainPrevious, reason, err := r.RegisterSession(t.Context(), tunnel1, TunnelStaleTimeout)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.False(t, drainPrevious)
	assert.Empty(t, reason)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.CloseNow() }()
	tunnel2 := newWebSocketAgentTunnel(envID, conn2)
	tunnel2.AgentInstance = "agent-a"
	tunnel2.SessionID = "session-b"
	accepted, drainPrevious, reason, err = r.RegisterSession(t.Context(), tunnel2, TunnelStaleTimeout)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.True(t, drainPrevious)
	assert.Empty(t, reason)
	assert.True(t, tunnel1.Conn.IsClosed())

	got, ok := r.Get(envID).Get()
	assert.True(t, ok)
	assert.Equal(t, tunnel2, got)
}

func TestActorTunnelRegistryPublishesReplacementBeforeClosingPreviousInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	registry, err := NewActorTunnelRegistry(t.Context(), runtime)
	require.NoError(t, err)

	previousConn := &controlledCloseTunnelConn{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
		closed:       make(chan struct{}),
	}
	previous := NewAgentTunnelWithConn("env-actor-registry", previousConn)
	previous.AgentInstance = "agent-a"
	accepted, _, reason, err := registry.RegisterSession(t.Context(), previous, TunnelStaleTimeout)
	require.NoError(t, err)
	require.True(t, accepted, reason)

	replacementConn := &controlledCloseTunnelConn{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
		closed:       make(chan struct{}),
	}
	close(replacementConn.releaseClose)
	replacement := NewAgentTunnelWithConn("env-actor-registry", replacementConn)
	replacement.AgentInstance = "agent-a"
	registered := make(chan bool, 1)
	go func() {
		accepted, _, _, registerErr := registry.RegisterSession(t.Context(), replacement, TunnelStaleTimeout)
		if !assert.NoError(t, registerErr) {
			return
		}
		registered <- accepted
	}()

	select {
	case <-previousConn.closeStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "replacement did not begin closing the previous tunnel")
	}
	active, ok := registry.Get("env-actor-registry").Get()
	require.True(t, ok)
	require.Same(t, replacement, active)
	close(previousConn.releaseClose)
	require.True(t, <-registered)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, registry.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestActorTunnelRegistryReportsStoppedExecutorAsInfrastructureFailureInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	registry, err := NewActorTunnelRegistry(t.Context(), runtime)
	require.NoError(t, err)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, registry.Stop(stopCtx))

	tunnel := NewAgentTunnelWithConn("env-stopped-registry", &controlledCloseTunnelConn{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
		closed:       make(chan struct{}),
	})
	accepted, drainPrevious, reason, err := registry.RegisterSession(t.Context(), tunnel, TunnelStaleTimeout)
	require.Error(t, err)
	require.False(t, accepted)
	require.False(t, drainPrevious)
	require.Empty(t, reason)
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestTunnelRegistry_CleanupStale(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)

	// Manually set last heartbeat to past
	tunnel.mu.Lock()
	tunnel.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	tunnel.mu.Unlock()

	r.Register(envID, tunnel)

	// Cleanup
	removed := r.CleanupStale(t.Context(), 5*time.Minute)
	require.Len(t, removed, 1)
	assert.Same(t, tunnel, removed[0])

	_, ok := r.Get(envID).Get()
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
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel("env-1", conn)

	initial := tunnel.GetLastHeartbeat()
	time.Sleep(10 * time.Millisecond)

	tunnel.UpdateHeartbeat()
	updated := tunnel.GetLastHeartbeat()

	assert.True(t, updated.After(initial))
}
