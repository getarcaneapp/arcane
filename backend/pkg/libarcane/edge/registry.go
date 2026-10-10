package edge

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/mo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
)

// NewAgentTunnelWithConn creates a new agent tunnel from a transport-agnostic connection.
func NewAgentTunnelWithConn(envID string, conn TunnelConnection) *AgentTunnel {
	now := time.Now()
	return &AgentTunnel{
		EnvironmentID: envID,
		Conn:          conn,
		ConnectedAt:   now,
		LastHeartbeat: now,
		State:         "connected",
		done:          make(chan struct{}),
	}
}

// UpdateHeartbeat updates the last heartbeat timestamp
func (t *AgentTunnel) UpdateHeartbeat() {
	t.mu.Lock()
	t.LastHeartbeat = time.Now()
	t.mu.Unlock()
}

// connected reports whether the tunnel has a live, open connection.
func (t *AgentTunnel) connected() bool {
	return t != nil && t.Conn != nil && !t.Conn.IsClosed()
}

// GetLastHeartbeat returns the last heartbeat time
func (t *AgentTunnel) GetLastHeartbeat() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.LastHeartbeat
}

// TunnelMetadataSnapshot is a consistent copy of a tunnel's mutable metadata.
type TunnelMetadataSnapshot struct {
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	SessionID     string
	AgentInstance string
	SecurityMode  string
	Capabilities  []string
	State         string
}

// MetadataSnapshot returns the tunnel's mutable metadata under one read lock.
// Reading the fields directly races with the writers that take t.mu (heartbeats,
// registration, CloseWithReason) and can mix state from either side of a close.
func (t *AgentTunnel) MetadataSnapshot() TunnelMetadataSnapshot {
	if t == nil {
		return TunnelMetadataSnapshot{}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return TunnelMetadataSnapshot{
		ConnectedAt:   t.ConnectedAt,
		LastHeartbeat: t.LastHeartbeat,
		SessionID:     t.SessionID,
		AgentInstance: t.AgentInstance,
		SecurityMode:  t.SecurityMode,
		Capabilities:  append([]string(nil), t.Capabilities...),
		State:         t.State,
	}
}

// CloseWithReason closes the tunnel connection and records the disconnect
// reason. Pass "" when there is no reason to record.
func (t *AgentTunnel) CloseWithReason(reason string) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.State = "closed"
	if reason != "" {
		t.DisconnectErr = reason
	}
	t.mu.Unlock()
	t.closeOnce.Do(func() {
		if t.done != nil {
			close(t.done)
		}
	})
	if t.Conn == nil {
		return nil
	}
	return t.Conn.Close()
}

// NewTunnelRegistry creates a new tunnel registry
func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{
		tunnels: concurrency.NewStateMap[string, *AgentTunnel](),
	}
}

// Get retrieves a tunnel by environment ID
func (r *TunnelRegistry) Get(envID string) mo.Option[*AgentTunnel] {
	tunnel, ok := r.tunnels.Get(envID)
	return mo.TupleToOption(tunnel, ok)
}

// Register adds a tunnel to the registry, closing any existing tunnel for the same env
func (r *TunnelRegistry) Register(envID string, tunnel *AgentTunnel) {
	ctx := context.Background() //nolint:forbidigo // Legacy registration path runs outside any request context.
	if r == nil || tunnel == nil {
		slog.ErrorContext(ctx, "Failed to register edge agent tunnel", "environmentId", envID, "error", "tunnel is required")
		return
	}
	previous, err := r.tunnels.ApplyTyped(ctx, "register edge tunnel", func(tunnels map[string]*AgentTunnel) (*AgentTunnel, bool, error) {
		if r.stopped.Load() {
			return nil, false, errors.New("edge tunnel registry stopped")
		}
		previous := tunnels[envID]
		tunnels[envID] = tunnel
		return previous, true, nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to register edge agent tunnel", "environmentId", envID, "error", err)
		return
	}
	if previous != nil && previous != tunnel {
		slog.InfoContext(ctx, "Replacing existing edge tunnel")
		_ = previous.CloseWithReason("")
	}
	slog.InfoContext(ctx, "Edge agent tunnel registered")
}

type registerSessionResult struct {
	accepted      bool
	drainPrevious bool
	reason        string
	previous      *AgentTunnel
}

// RegisterSession adds a tunnel with duplicate-session handling.
func (r *TunnelRegistry) RegisterSession(ctx context.Context, tunnel *AgentTunnel, staleAfter time.Duration) (bool, bool, string, error) {
	if r == nil || tunnel == nil {
		return false, false, "tunnel is required", nil
	}

	envID := tunnel.EnvironmentID
	if envID == "" {
		return false, false, "environment ID is required", nil
	}

	result, err := r.tunnels.ApplyTyped(ctx, "register edge tunnel session", func(tunnels map[string]*AgentTunnel) (registerSessionResult, bool, error) {
		var result registerSessionResult
		if r.stopped.Load() {
			return result, false, errors.New("edge tunnel registry stopped")
		}
		if existing := tunnels[envID]; existing != nil {
			if existing == tunnel {
				result.accepted = true
				return result, false, nil
			}

			existingMetadata := existing.MetadataSnapshot()
			tunnelMetadata := tunnel.MetadataSnapshot()
			existingStale := staleAfter > 0 && time.Since(existingMetadata.LastHeartbeat) > staleAfter
			sameAgentInstance := existingMetadata.AgentInstance != "" && tunnelMetadata.AgentInstance != "" && existingMetadata.AgentInstance == tunnelMetadata.AgentInstance
			if existing.connected() && !existingStale && !sameAgentInstance {
				result.reason = "another edge agent session is already active"
				return result, false, nil
			}
			result.previous = existing
			result.drainPrevious = sameAgentInstance
		}

		tunnels[envID] = tunnel
		result.accepted = true
		return result, true, nil
	})
	if err != nil {
		return false, false, "", err
	}
	if result.previous != nil {
		_ = result.previous.CloseWithReason("replaced by newer edge tunnel session")
	}
	metadata := tunnel.MetadataSnapshot()
	slog.InfoContext(ctx, "Edge agent tunnel registered", "environmentId", envID, "sessionId", metadata.SessionID, "securityMode", metadata.SecurityMode)
	return result.accepted, result.drainPrevious, result.reason, nil
}

// Unregister removes a tunnel from the registry
func (r *TunnelRegistry) Unregister(envID string) {
	ctx := context.Background() //nolint:forbidigo // Legacy unregistration path runs outside any request context.
	tunnel, removed, err := r.tunnels.Remove(ctx, "unregister edge tunnel", envID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to unregister edge agent tunnel", "environmentId", envID, "error", err)
		return
	}
	if removed {
		_ = tunnel.CloseWithReason("")
		slog.InfoContext(ctx, "Edge agent tunnel unregistered")
	}
}

// UnregisterCurrent removes the active tunnel only when it still matches the
// provided tunnel reference. It reports whether the tunnel was removed and
// whether another active session for the environment is already present.
func (r *TunnelRegistry) UnregisterCurrent(ctx context.Context, envID string, current *AgentTunnel) (bool, bool) {
	if r == nil || current == nil {
		return false, false
	}

	type unregisterResult struct {
		removed           bool
		activeReplacement bool
	}
	result, err := r.tunnels.ApplyTyped(ctx, "unregister current edge tunnel", func(tunnels map[string]*AgentTunnel) (unregisterResult, bool, error) {
		var result unregisterResult
		existing := tunnels[envID]
		if existing != current {
			result.activeReplacement = existing.connected()
			return result, false, nil
		}
		delete(tunnels, envID)
		result.removed = true
		return result, true, nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to unregister current edge agent tunnel", "environmentId", envID, "error", err)
		return false, false
	}
	if !result.removed {
		return false, result.activeReplacement
	}

	_ = current.CloseWithReason("")
	metadata := current.MetadataSnapshot()
	slog.InfoContext(ctx, "Edge agent tunnel unregistered", "environmentId", envID, "sessionId", metadata.SessionID)
	return true, false
}

// CleanupStale removes tunnels that haven't had a heartbeat within the given duration.
func (r *TunnelRegistry) CleanupStale(ctx context.Context, maxAge time.Duration) []*AgentTunnel {
	now := time.Now()
	removed, err := r.tunnels.RemoveWhere(ctx, "clean up stale edge tunnels", func(_ string, tunnel *AgentTunnel) bool {
		return tunnel == nil || now.Sub(tunnel.GetLastHeartbeat()) > maxAge
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to clean up stale edge tunnels", "error", err)
		return nil
	}
	for _, tunnel := range removed {
		if tunnel == nil {
			continue
		}
		slog.WarnContext(ctx, "Removing stale edge tunnel", "lastHeartbeat", tunnel.GetLastHeartbeat())
		_ = tunnel.CloseWithReason("edge tunnel heartbeat expired")
	}

	return removed
}

// Stop drains registry mutations and closes active tunnels.
func (r *TunnelRegistry) Stop(ctx context.Context) error {
	r.stopped.Store(true)
	fallback := r.tunnels.Values()
	removed, err := r.tunnels.Drain(ctx, "stop edge tunnel registry")
	if err != nil {
		removed = fallback
	}
	for _, tunnel := range removed {
		if tunnel != nil {
			err = errors.Join(err, tunnel.CloseWithReason("edge tunnel registry stopped"))
		}
	}
	return err
}

// ObserveAgents reports connected edge agents by transport through the arcane.edge.agents gauge.
// Poll-mode agents count as "poll" while their check-ins are fresh and no tunnel is open.
func (r *TunnelRegistry) ObserveAgents(meter metric.Meter) (metric.Registration, error) {
	gauge, err := meter.Int64ObservableGauge("arcane.edge.agents",
		metric.WithDescription("Connected edge agents by transport"),
		metric.WithUnit("{agent}"),
	)
	if err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		counts := map[string]int64{EdgeTransportWebSocket: 0, EdgeTransportGRPC: 0, EdgeTransportPoll: 0}
		tunneled := make(map[string]bool)
		for _, tunnel := range r.tunnels.Values() {
			if tunnel.connected() {
				counts[tunnel.Conn.Transport()]++
				tunneled[tunnel.EnvironmentID] = true
			}
		}
		polls, now := GetPollRuntimeRegistry(), time.Now()
		polls.mu.RLock()
		for envID, state := range polls.states {
			if !tunneled[envID] && state.LastPollAt != nil && now.Sub(*state.LastPollAt) <= pollRuntimeTTL(state) {
				counts[EdgeTransportPoll]++
			}
		}
		polls.mu.RUnlock()
		for transport, count := range counts {
			o.ObserveInt64(gauge, count, metric.WithAttributes(attribute.String("arcane.edge.transport", transport)))
		}
		return nil
	}, gauge)
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

// ClearDefaultRegistry replaces registry only when it is still the active default.
func ClearDefaultRegistry(registry *TunnelRegistry) {
	defaultRegistryMu.Lock()
	if defaultRegistry == registry {
		defaultRegistry = NewTunnelRegistry()
	}
	defaultRegistryMu.Unlock()
}
