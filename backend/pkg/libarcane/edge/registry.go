package edge

import (
	"log/slog"
	"sync"
	"time"
)

// AgentTunnel represents an active tunnel connection from an edge agent
type AgentTunnel struct {
	EnvironmentID string
	Conn          TunnelConnection
	Pending       sync.Map // map[string]*PendingRequest
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	SessionID     string
	AgentInstance string
	Transport     string
	SecurityMode  string
	Capabilities  []string
	State         string
	DisconnectErr string
	mu            sync.RWMutex
}

// NewAgentTunnelWithConn creates a new agent tunnel from a transport-agnostic connection.
func NewAgentTunnelWithConn(envID string, conn TunnelConnection) *AgentTunnel {
	now := time.Now()
	return &AgentTunnel{
		EnvironmentID: envID,
		Conn:          conn,
		ConnectedAt:   now,
		LastHeartbeat: now,
		State:         "connected",
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
	return t.CloseWithReason("")
}

// CloseWithReason closes the tunnel connection and records the disconnect reason.
func (t *AgentTunnel) CloseWithReason(reason string) error {
	t.mu.Lock()
	t.State = "closed"
	if reason != "" {
		t.DisconnectErr = reason
	}
	t.mu.Unlock()
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

// RegisterSession adds a tunnel with duplicate-session handling.
func (r *TunnelRegistry) RegisterSession(tunnel *AgentTunnel, staleAfter time.Duration) (accepted bool, drainPrevious bool, reason string) {
	if r == nil || tunnel == nil {
		return false, false, "tunnel is required"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	envID := tunnel.EnvironmentID
	if envID == "" {
		return false, false, "environment ID is required"
	}

	if existing, ok := r.tunnels[envID]; ok && existing != nil {
		if existing == tunnel {
			return true, false, ""
		}

		existingStale := staleAfter > 0 && time.Since(existing.GetLastHeartbeat()) > staleAfter
		sameAgentInstance := existing.AgentInstance != "" && tunnel.AgentInstance != "" && existing.AgentInstance == tunnel.AgentInstance
		legacyReplace := existing.AgentInstance == "" || tunnel.AgentInstance == ""

		if existing.Conn == nil || existing.Conn.IsClosed() || existingStale || sameAgentInstance || legacyReplace {
			if sameAgentInstance {
				drainPrevious = true
			}
			_ = existing.CloseWithReason("replaced by newer edge tunnel session")
		} else {
			return false, false, "another edge agent session is already active"
		}
	}

	r.tunnels[envID] = tunnel
	slog.Info("Edge agent tunnel registered", "environment_id", envID, "session_id", tunnel.SessionID, "security_mode", tunnel.SecurityMode)
	return true, drainPrevious, ""
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

// UnregisterCurrent removes the active tunnel only when it still matches the
// provided tunnel reference. It reports whether the tunnel was removed and
// whether another active session for the environment is already present.
func (r *TunnelRegistry) UnregisterCurrent(envID string, current *AgentTunnel) (removed bool, activeReplacement bool) {
	if r == nil || current == nil {
		return false, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.tunnels[envID]
	if !ok || existing != current {
		if ok && existing != nil && !existing.Conn.IsClosed() {
			return false, true
		}
		return false, false
	}

	_ = existing.Close()
	delete(r.tunnels, envID)
	slog.Info("Edge agent tunnel unregistered", "environment_id", envID, "session_id", current.SessionID)
	return true, false
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
