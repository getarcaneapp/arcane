package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/mo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"kit.getarcane.app/concurrency"
)

type (
	// TunnelMetadataSnapshot is a consistent copy of a tunnel's mutable metadata.
	TunnelMetadataSnapshot struct {
		ConnectedAt      time.Time
		LastHeartbeat    time.Time
		Host             *AgentHostInfo
		SessionID        string
		AgentInstance    string
		SecurityMode     string
		State            string
		RemoteAddr       string
		DisconnectReason string
		Capabilities     []string
	}

	registerSessionResult struct {
		previous      *AgentTunnel
		reason        string
		accepted      bool
		stored        bool
		drainPrevious bool
	}
)

var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   = NewTunnelRegistry()
)

// NewAgentTunnelWithConn creates a new agent tunnel from a transport-agnostic connection. Its mux session
// exists before registration so callers that find the tunnel mid-handshake wait for it.
func NewAgentTunnelWithConn(envID string, conn TunnelConnection) *AgentTunnel {
	now := time.Now()
	mux := &muxSession{ready: make(chan struct{}), closed: make(chan struct{})}
	return &AgentTunnel{
		EnvironmentID: envID,
		Conn:          conn,
		ConnectedAt:   now,
		LastHeartbeat: now,
		State:         "connected",
		HTTPClient:    newMuxHTTPClient(mux.dial),
		done:          make(chan struct{}),
		mux:           mux,
	}
}

// UpdateHeartbeat updates the last heartbeat timestamp
func (t *AgentTunnel) UpdateHeartbeat() {
	t.mu.Lock()
	t.LastHeartbeat = time.Now()
	t.mu.Unlock()
}

// openStreams counts open mux streams.
func (t *AgentTunnel) openStreams() int {
	t.mux.mu.Lock()
	defer t.mux.mu.Unlock()
	if t.mux.session == nil {
		return 0
	}
	return t.mux.session.NumStreams()
}

// GetLastHeartbeat returns the last heartbeat time
func (t *AgentTunnel) GetLastHeartbeat() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.LastHeartbeat
}

// MetadataSnapshot returns the tunnel's mutable metadata under one read lock, so readers never race
// heartbeat, registration, or close writers.
func (t *AgentTunnel) MetadataSnapshot() TunnelMetadataSnapshot {
	if t == nil {
		return TunnelMetadataSnapshot{}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return TunnelMetadataSnapshot{
		ConnectedAt:      t.ConnectedAt,
		LastHeartbeat:    t.LastHeartbeat,
		Host:             t.Host,
		SessionID:        t.SessionID,
		AgentInstance:    t.AgentInstance,
		SecurityMode:     t.SecurityMode,
		Capabilities:     append([]string(nil), t.Capabilities...),
		State:            t.State,
		RemoteAddr:       t.RemoteAddr,
		DisconnectReason: t.DisconnectErr,
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
	t.mux.close()
	if t.Conn == nil {
		return nil
	}
	return t.Conn.Close()
}

// Active returns the environment's tunnel when its connection is still open.
func (r *TunnelRegistry) Active(envID string) mo.Option[*AgentTunnel] {
	if tunnel, _ := r.Get(envID).Get(); tunnel != nil && tunnel.Conn != nil && !tunnel.Conn.IsClosed() {
		return mo.Some(tunnel)
	}
	return mo.None[*AgentTunnel]()
}

// WaitForActive waits until the environment has an open tunnel or timeout elapses.
func (r *TunnelRegistry) WaitForActive(ctx context.Context, envID string, timeout time.Duration) mo.Option[*AgentTunnel] {
	if tunnel := r.Active(envID); tunnel.IsPresent() {
		return tunnel
	}
	registered := make(chan struct{}, 1)
	unsubscribe := r.registered.Subscribe(func(id string) {
		if id != envID {
			return
		}
		select {
		case registered <- struct{}{}:
		default:
		}
	})
	defer unsubscribe()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if tunnel := r.Active(envID); tunnel.IsPresent() {
			return tunnel
		}
		select {
		case <-waitCtx.Done():
			return mo.None[*AgentTunnel]()
		case <-registered:
		}
	}
}

// NewTunnelRegistry creates a new tunnel registry
func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{
		tunnels:    concurrency.NewStateMap[string, *AgentTunnel](),
		history:    concurrency.NewStateMap[string, TunnelHistory](),
		registered: concurrency.NewSignal[string](),
	}
}

func (r *TunnelRegistry) recordSession(ctx context.Context, envID string) {
	if err := r.history.Apply(ctx, "record agent tunnel session", func(history map[string]TunnelHistory) (bool, error) {
		previous := history[envID]
		history[envID] = TunnelHistory{
			LastDisconnectAt:     previous.LastDisconnectAt,
			LastDisconnectReason: previous.LastDisconnectReason,
			Sessions:             previous.Sessions + 1,
		}
		return true, nil
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to record agent tunnel session", "environmentId", envID, "error", err)
	}
}

func (r *TunnelRegistry) recordDisconnect(ctx context.Context, tunnel *AgentTunnel) {
	reason := tunnel.MetadataSnapshot().DisconnectReason
	if err := r.history.Apply(ctx, "record agent tunnel disconnect", func(history map[string]TunnelHistory) (bool, error) {
		history[tunnel.EnvironmentID] = TunnelHistory{
			LastDisconnectAt:     time.Now(),
			LastDisconnectReason: reason,
			Sessions:             history[tunnel.EnvironmentID].Sessions,
		}
		return true, nil
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to record agent tunnel disconnect", "environmentId", tunnel.EnvironmentID, "error", err)
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
		slog.ErrorContext(ctx, "Failed to register agent tunnel", "environmentId", envID, "error", "tunnel is required")
		return
	}
	previous, err := r.tunnels.ApplyTyped(ctx, "register agent tunnel", func(tunnels map[string]*AgentTunnel) (*AgentTunnel, bool, error) {
		if r.stopped.Load() {
			return nil, false, errors.New("agent tunnel registry stopped")
		}
		previous := tunnels[envID]
		tunnels[envID] = tunnel
		return previous, true, nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to register agent tunnel", "environmentId", envID, "error", err)
		return
	}
	if previous != nil && previous != tunnel {
		slog.InfoContext(ctx, "Replacing existing agent tunnel")
		_ = previous.CloseWithReason("")
	}
	r.registered.Publish(envID)
	slog.InfoContext(ctx, "Agent tunnel registered")
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
	if existing, ok := r.Get(envID).Get(); ok && existing != tunnel && (existing != nil && existing.Conn != nil && !existing.Conn.IsClosed()) && !existing.mux.alive(ctx, muxRegistrationPingTimeout) {
		slog.InfoContext(ctx, "Existing agent tunnel session failed a liveness ping; replacing it", "environmentId", envID)
		_ = existing.CloseWithReason("agent tunnel liveness ping failed")
	}

	result, err := r.tunnels.ApplyTyped(ctx, "register agent tunnel session", func(tunnels map[string]*AgentTunnel) (registerSessionResult, bool, error) {
		if r.stopped.Load() {
			return registerSessionResult{}, false, errors.New("agent tunnel registry stopped")
		}
		existing := tunnels[envID]
		if existing == tunnel {
			return registerSessionResult{accepted: true}, false, nil
		}

		existingMetadata := existing.MetadataSnapshot()
		sameAgentInstance := existingMetadata.AgentInstance != "" && existingMetadata.AgentInstance == tunnel.MetadataSnapshot().AgentInstance
		if existing != nil && existing.Conn != nil && !existing.Conn.IsClosed() &&
			(staleAfter <= 0 || time.Since(existingMetadata.LastHeartbeat) <= staleAfter) && !sameAgentInstance {
			return registerSessionResult{reason: "another agent session is already active"}, false, nil
		}

		tunnels[envID] = tunnel
		return registerSessionResult{
			previous:      existing,
			accepted:      true,
			stored:        true,
			drainPrevious: sameAgentInstance,
		}, true, nil
	})
	if err != nil {
		return false, false, "", err
	}
	if result.previous != nil {
		_ = result.previous.CloseWithReason("replaced by newer agent tunnel session")
		r.recordDisconnect(ctx, result.previous)
	}
	if result.stored {
		r.recordSession(ctx, envID)
		r.registered.Publish(envID)
	}
	metadata := tunnel.MetadataSnapshot()
	slog.InfoContext(ctx, "Agent tunnel registered", "environmentId", envID, "sessionId", metadata.SessionID, "securityMode", metadata.SecurityMode)
	return result.accepted, result.drainPrevious, result.reason, nil
}

// Unregister removes a tunnel from the registry
func (r *TunnelRegistry) Unregister(envID string) {
	ctx := context.Background() //nolint:forbidigo // Legacy unregistration path runs outside any request context.
	tunnel, removed, err := r.tunnels.Remove(ctx, "unregister agent tunnel", envID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to unregister agent tunnel", "environmentId", envID, "error", err)
		return
	}
	if removed {
		_ = tunnel.CloseWithReason("")
		r.recordDisconnect(ctx, tunnel)
		slog.InfoContext(ctx, "Agent tunnel unregistered")
	}
}

// UnregisterCurrent removes the environment's tunnel only if it is still current. It reports whether it was
// removed and whether another live session already replaced it.
func (r *TunnelRegistry) UnregisterCurrent(ctx context.Context, envID string, current *AgentTunnel) (bool, bool) {
	if r == nil || current == nil {
		return false, false
	}

	type unregisterResult struct {
		removed           bool
		activeReplacement bool
	}
	result, err := r.tunnels.ApplyTyped(ctx, "unregister current agent tunnel", func(tunnels map[string]*AgentTunnel) (unregisterResult, bool, error) {
		existing := tunnels[envID]
		if existing != current {
			return unregisterResult{activeReplacement: existing != nil && existing.Conn != nil && !existing.Conn.IsClosed()}, false, nil
		}
		delete(tunnels, envID)
		return unregisterResult{removed: true}, true, nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to unregister current agent tunnel", "environmentId", envID, "error", err)
		return false, false
	}
	if !result.removed {
		return false, result.activeReplacement
	}

	_ = current.CloseWithReason("")
	r.recordDisconnect(ctx, current)
	metadata := current.MetadataSnapshot()
	slog.InfoContext(ctx, "Agent tunnel unregistered", "environmentId", envID, "sessionId", metadata.SessionID)
	return true, false
}

// CleanupStale removes tunnels that haven't had a heartbeat within the given duration.
func (r *TunnelRegistry) CleanupStale(ctx context.Context, maxAge time.Duration) []*AgentTunnel {
	now := time.Now()
	removed, err := r.tunnels.RemoveWhere(ctx, "clean up stale agent tunnels", func(_ string, tunnel *AgentTunnel) bool {
		return tunnel == nil || now.Sub(tunnel.GetLastHeartbeat()) > maxAge
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to clean up stale agent tunnels", "error", err)
		return nil
	}
	for _, tunnel := range removed {
		if tunnel == nil {
			continue
		}
		slog.WarnContext(ctx, "Removing stale agent tunnel", "lastHeartbeat", tunnel.GetLastHeartbeat())
		_ = tunnel.CloseWithReason("agent tunnel heartbeat expired")
		r.recordDisconnect(ctx, tunnel)
	}

	return removed
}

// Stop drains registry mutations and closes active tunnels.
func (r *TunnelRegistry) Stop(ctx context.Context) error {
	r.stopped.Store(true)
	fallback := r.tunnels.Values()
	removed, err := r.tunnels.RemoveWhere(ctx, "stop agent tunnel registry", func(string, *AgentTunnel) bool {
		return true
	})
	if err != nil {
		removed = fallback
	}
	for _, tunnel := range removed {
		if tunnel != nil {
			err = errors.Join(err, tunnel.CloseWithReason("agent tunnel registry stopped"))
		}
	}
	return err
}

// ObserveAgents reports connected agents by transport through the arcane.agents gauge.
func (r *TunnelRegistry) ObserveAgents(meter metric.Meter) (metric.Registration, error) {
	gauge, err := meter.Int64ObservableGauge("arcane.agents",
		metric.WithDescription("Connected agents by transport"),
		metric.WithUnit("{agent}"),
	)
	if err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		counts := map[string]int64{TransportWebSocket: 0, TransportGRPC: 0}
		for _, tunnel := range r.tunnels.Values() {
			if tunnel != nil && tunnel.Conn != nil && !tunnel.Conn.IsClosed() {
				counts[tunnel.Conn.Transport()]++
			}
		}
		for transport, count := range counts {
			o.ObserveInt64(gauge, count, metric.WithAttributes(attribute.String("arcane.agent.transport", transport)))
		}
		return nil
	}, gauge)
}

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
