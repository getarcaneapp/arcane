package agent

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	controlledCloseTunnelConn struct {
		closeStarted chan struct{}
		releaseClose chan struct{}
		closed       chan struct{}
	}
)

func (c *controlledCloseTunnelConn) Send(*TunnelMessage) error { return nil }

func (c *controlledCloseTunnelConn) Receive() (*TunnelMessage, error) { return nil, nil }

func (c *controlledCloseTunnelConn) muxConn(context.Context) *muxConn { return nil }

func (c *controlledCloseTunnelConn) Transport() string { return TransportWebSocket }

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

func createTestConn(t testing.TB) *websocket.Conn {
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

	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)

	registerTestTunnel(t, r, tunnel)

	got, ok := r.Get(envID).Get()
	assert.True(t, ok)
	assert.Equal(t, tunnel, got)

	r.Unregister(envID)
	_, ok = r.Get(envID).Get()
	assert.False(t, ok)

	assert.True(t, tunnel.Conn.IsClosed())
}

func TestTunnelRegistry_RegisterSessionRejectsCompetingAgent(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-session-reject"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.CloseNow() }()
	tunnel1 := NewAgentTunnel(AgentTunnelConfig{
		Conn:          NewWebSocketTunnelConn(conn1),
		EnvironmentID: envID,
		SessionID:     "session-a",
		AgentInstance: "agent-a",
	})

	result, err := r.RegisterSession(t.Context(), tunnel1)
	require.NoError(t, err)
	assert.Equal(t, RegisterResult{Accepted: true}, result)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.CloseNow() }()
	tunnel2 := NewAgentTunnel(AgentTunnelConfig{
		Conn:          NewWebSocketTunnelConn(conn2),
		EnvironmentID: envID,
		SessionID:     "session-b",
		AgentInstance: "agent-b",
	})

	result, err = r.RegisterSession(t.Context(), tunnel2)
	require.NoError(t, err)
	assert.Equal(t, RegisterResult{Reason: "another agent session is already active"}, result)

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
	tunnel1 := NewAgentTunnel(AgentTunnelConfig{
		Conn:          NewWebSocketTunnelConn(conn1),
		EnvironmentID: envID,
		SessionID:     "session-a",
		AgentInstance: "agent-a",
	})
	result, err := r.RegisterSession(t.Context(), tunnel1)
	require.NoError(t, err)
	assert.Equal(t, RegisterResult{Accepted: true}, result)

	conn2 := createTestConn(t)
	defer func() { _ = conn2.CloseNow() }()
	tunnel2 := NewAgentTunnel(AgentTunnelConfig{
		Conn:          NewWebSocketTunnelConn(conn2),
		EnvironmentID: envID,
		SessionID:     "session-b",
		AgentInstance: "agent-a",
	})
	result, err = r.RegisterSession(t.Context(), tunnel2)
	require.NoError(t, err)
	assert.Equal(t, RegisterResult{Accepted: true, DrainPrevious: true}, result)
	assert.True(t, tunnel1.Conn.IsClosed())

	got, ok := r.Get(envID).Get()
	assert.True(t, ok)
	assert.Equal(t, tunnel2, got)
}

func TestTunnelRegistryPublishesReplacementBeforeClosingPrevious(t *testing.T) {
	registry := NewTunnelRegistry()

	previousConn := &controlledCloseTunnelConn{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
		closed:       make(chan struct{}),
	}
	previous := NewAgentTunnel(AgentTunnelConfig{
		Conn:          previousConn,
		EnvironmentID: "env-actor-registry",
		AgentInstance: "agent-a",
	})
	result, err := registry.RegisterSession(t.Context(), previous)
	require.NoError(t, err)
	require.True(t, result.Accepted, result.Reason)

	replacementConn := &controlledCloseTunnelConn{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
		closed:       make(chan struct{}),
	}
	close(replacementConn.releaseClose)
	replacement := NewAgentTunnel(AgentTunnelConfig{
		Conn:          replacementConn,
		EnvironmentID: "env-actor-registry",
		AgentInstance: "agent-a",
	})
	registered := make(chan bool, 1)
	go func() {
		localResult, registerErr := registry.RegisterSession(t.Context(), replacement)
		if !assert.NoError(t, registerErr) {
			return
		}
		registered <- localResult.Accepted
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

	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, registry.Stop(stopCtx))
}

func TestTunnelRegistryRejectsRegistrationAfterStop(t *testing.T) {
	registry := NewTunnelRegistry()

	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, registry.Stop(stopCtx))

	tunnel := NewAgentTunnel(AgentTunnelConfig{
		Conn: &controlledCloseTunnelConn{
			closeStarted: make(chan struct{}),
			releaseClose: make(chan struct{}),
			closed:       make(chan struct{}),
		},
		EnvironmentID: "env-stopped-registry",
	})
	result, err := registry.RegisterSession(t.Context(), tunnel)
	require.Error(t, err)
	require.Equal(t, RegisterResult{}, result)
}

func TestTunnelRegistry_CleanupStale(t *testing.T) {
	r := NewTunnelRegistry()
	envID := "env-1"

	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)

	tunnel.mu.Lock()
	tunnel.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	tunnel.mu.Unlock()

	registerTestTunnel(t, r, tunnel)

	removed := r.CleanupStale(t.Context(), 5*time.Minute)
	require.Len(t, removed, 1)
	assert.Same(t, tunnel, removed[0])

	_, ok := r.Get(envID).Get()
	assert.False(t, ok)
	assert.True(t, tunnel.Conn.IsClosed())
}

func TestTunnelRegistry_RecordsSessionHistory(t *testing.T) {
	registry := NewTunnelRegistry()
	envID := "env-history"

	conn1 := createTestConn(t)
	defer func() { _ = conn1.CloseNow() }()
	tunnel1 := newWebSocketAgentTunnel(envID, conn1)
	result, err := registry.RegisterSession(t.Context(), tunnel1)
	require.NoError(t, err)
	require.True(t, result.Accepted)

	history, ok := registry.History(envID).Get()
	require.True(t, ok)
	assert.Equal(t, 1, history.Sessions)
	assert.True(t, history.LastDisconnectAt.IsZero())

	before := time.Now()
	require.NoError(t, tunnel1.CloseWithReason("agent tunnel keepalive failed"))
	require.True(t, registry.UnregisterCurrent(t.Context(), tunnel1))

	history, ok = registry.History(envID).Get()
	require.True(t, ok)
	assert.Equal(t, 1, history.Sessions)
	assert.Equal(t, "agent tunnel keepalive failed", history.LastDisconnectReason)
	assert.False(t, history.LastDisconnectAt.Before(before))

	conn2 := createTestConn(t)
	defer func() { _ = conn2.CloseNow() }()
	result, err = registry.RegisterSession(t.Context(), newWebSocketAgentTunnel(envID, conn2))
	require.NoError(t, err)
	require.True(t, result.Accepted)

	history, ok = registry.History(envID).Get()
	require.True(t, ok)
	assert.Equal(t, 2, history.Sessions)
	assert.Equal(t, "agent tunnel keepalive failed", history.LastDisconnectReason)
	assert.True(t, registry.History("env-unknown").IsAbsent())
}

func TestTunnelRegistry_RecordsStaleCleanupDisconnect(t *testing.T) {
	registry := NewTunnelRegistry()
	envID := "env-stale-history"

	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel(envID, conn)
	result, err := registry.RegisterSession(t.Context(), tunnel)
	require.NoError(t, err)
	require.True(t, result.Accepted)

	tunnel.mu.Lock()
	tunnel.LastHeartbeat = time.Now().Add(-time.Hour)
	tunnel.mu.Unlock()
	require.Len(t, registry.CleanupStale(t.Context(), time.Minute), 1)

	history, ok := registry.history.Get(envID)
	require.True(t, ok)
	assert.Equal(t, 1, history.Sessions)
	assert.Equal(t, "agent tunnel heartbeat expired", history.LastDisconnectReason)
	assert.False(t, history.LastDisconnectAt.IsZero())
}

func BenchmarkWaitForActive(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	r := NewTunnelRegistry()
	conn := createTestConn(b)
	b.Cleanup(func() { _ = conn.CloseNow() })
	registerTestTunnel(b, r, newWebSocketAgentTunnel("env-1", conn))

	b.ReportAllocs()
	for b.Loop() {
		if !r.WaitForActive(b.Context(), "env-1", WaitForActiveOptions{Timeout: DefaultTunnelAcquireTimeout}).IsPresent() {
			b.Fatal("tunnel not active")
		}
	}
}

func BenchmarkWaitForActiveReconnect(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	r := NewTunnelRegistry()
	conn := createTestConn(b)
	b.Cleanup(func() { _ = conn.CloseNow() })
	tunnel := newWebSocketAgentTunnel("env-1", conn)

	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = r.tunnels.Remove(b.Context(), "reset benchmark tunnel", "env-1")
		time.AfterFunc(time.Millisecond, func() {
			_, _ = r.RegisterSession(b.Context(), tunnel)
		})
		if !r.WaitForActive(b.Context(), "env-1", WaitForActiveOptions{Timeout: DefaultTunnelAcquireTimeout}).IsPresent() {
			b.Fatal("tunnel not active")
		}
	}
}

func TestTunnelRegistryActive(t *testing.T) {
	tunnels := NewTunnelRegistry()
	conn := createTestConn(t)
	defer func() { _ = conn.CloseNow() }()
	tunnel := newWebSocketAgentTunnel("env-active", conn)
	registerTestTunnel(t, tunnels, tunnel)
	defer tunnels.Unregister("env-active")

	assert.True(t, tunnels.Active("env-active").IsPresent())
	assert.False(t, tunnels.Active("non-existent").IsPresent())

	_ = tunnel.CloseWithReason("")
	assert.False(t, tunnels.Active("env-active").IsPresent())
}

func registerTestTunnel(tb testing.TB, registry *TunnelRegistry, tunnel *AgentTunnel) {
	tb.Helper()
	result, err := registry.RegisterSession(tb.Context(), tunnel)
	require.NoError(tb, err)
	require.True(tb, result.Accepted, result.Reason)
}
