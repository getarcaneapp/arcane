package edge

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// AgentTunnel represents an active tunnel connection from an edge agent
type AgentTunnel struct {
	EnvironmentID string
	Conn          TunnelConnection
	Pending       sync.Map // map[string]*PendingRequest
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	mu            sync.RWMutex
}

// NewAgentTunnel creates a new agent tunnel.
func NewAgentTunnel(envID string, conn *websocket.Conn) *AgentTunnel {
	return NewAgentTunnelWithConn(envID, NewTunnelConn(conn))
}

// NewAgentTunnelWithConn creates a new agent tunnel from a transport-agnostic connection.
func NewAgentTunnelWithConn(envID string, conn TunnelConnection) *AgentTunnel {
	now := time.Now()
	return &AgentTunnel{
		EnvironmentID: envID,
		Conn:          conn,
		ConnectedAt:   now,
		LastHeartbeat: now,
	}
}

// UpdateHeartbeat updates the last heartbeat timestamp
func (t *AgentTunnel) UpdateHeartbeat() {
	t.mu.Lock()
	t.LastHeartbeat = time.Now()
	t.mu.Unlock()
}

// GetLastHeartbeat returns the last heartbeat time
func (t *AgentTunnel) GetLastHeartbeat() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.LastHeartbeat
}

// Close closes the tunnel connection
func (t *AgentTunnel) Close() error {
	return t.Conn.Close()
}

// TunnelRegistry manages active edge agent tunnel connections
type TunnelRegistry struct {
	tunnels map[string]*AgentTunnel // environmentID -> tunnel
	mu      sync.RWMutex
}

// NewTunnelRegistry creates a new tunnel registry
func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{
		tunnels: make(map[string]*AgentTunnel),
	}
}

// Get retrieves a tunnel by environment ID
func (r *TunnelRegistry) Get(envID string) (*AgentTunnel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tunnel, ok := r.tunnels[envID]
	return tunnel, ok
}

// Register adds a tunnel to the registry, closing any existing tunnel for the same env
func (r *TunnelRegistry) Register(envID string, tunnel *AgentTunnel) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close existing tunnel if any
	if existing, ok := r.tunnels[envID]; ok {
		slog.Info("Replacing existing edge tunnel")
		_ = existing.Close()
	}

	r.tunnels[envID] = tunnel
	slog.Info("Edge agent tunnel registered")
}

// Unregister removes a tunnel from the registry
func (r *TunnelRegistry) Unregister(envID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if tunnel, ok := r.tunnels[envID]; ok {
		_ = tunnel.Close()
		delete(r.tunnels, envID)
		slog.Info("Edge agent tunnel unregistered")
	}
}

// CleanupStale removes tunnels that haven't had a heartbeat within the given duration
func (r *TunnelRegistry) CleanupStale(maxAge time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	removed := 0

	for envID, tunnel := range r.tunnels {
		if now.Sub(tunnel.GetLastHeartbeat()) > maxAge {
			slog.Warn("Removing stale edge tunnel", "last_heartbeat", tunnel.GetLastHeartbeat())
			_ = tunnel.Close()
			delete(r.tunnels, envID)
			removed++
		}
	}

	return removed
}

var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   = NewTunnelRegistry()
)

// GetRegistry returns the global tunnel registry
func GetRegistry() *TunnelRegistry {
	defaultRegistryMu.RLock()
	registry := defaultRegistry
	defaultRegistryMu.RUnlock()
	return registry
}

// SetDefaultRegistry replaces the process-wide default tunnel registry.
func SetDefaultRegistry(registry *TunnelRegistry) {
	if registry == nil {
		registry = NewTunnelRegistry()
	}

	defaultRegistryMu.Lock()
	defaultRegistry = registry
	defaultRegistryMu.Unlock()
}
