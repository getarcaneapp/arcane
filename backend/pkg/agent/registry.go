package agent

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/samber/mo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
)

type (
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

	AgentTunnelConfig struct {
		Conn          TunnelConnection
		Host          *AgentHostInfo
		EnvironmentID string
		SessionID     string
		AgentInstance string
		SecurityMode  string
		RemoteAddr    string
		Capabilities  []string
	}

	WaitForActiveOptions struct {
		Except  *AgentTunnel
		Timeout time.Duration
	}

	RegisterResult struct {
		Reason        string
		Accepted      bool
		DrainPrevious bool
	}

	registerSessionResult struct {
		previous *AgentTunnel
		RegisterResult

		stored bool
	}
)

func NewAgentTunnel(cfg AgentTunnelConfig) *AgentTunnel {
	now := time.Now()
	mux := &muxSession{
		ready:  make(chan struct{}),
		closed: make(chan struct{}),
	}
	return &AgentTunnel{
		ConnectedAt:   now,
		LastHeartbeat: now,
		Conn:          cfg.Conn,
		HTTPClient:    newMuxHTTPClient(mux.dial),
		done:          make(chan struct{}),
		mux:           mux,
		Host:          cfg.Host,
		EnvironmentID: cfg.EnvironmentID,
		SessionID:     cmp.Or(cfg.SessionID, uuid.New().String()),
		AgentInstance: cfg.AgentInstance,
		SecurityMode:  cfg.SecurityMode,
		State:         "connected",
		RemoteAddr:    cfg.RemoteAddr,
		Capabilities:  cfg.Capabilities,
	}
}

func (t *AgentTunnel) UpdateHeartbeat() {
	t.mu.Lock()
	t.LastHeartbeat = time.Now()
	t.mu.Unlock()
}

func (t *AgentTunnel) GetLastHeartbeat() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.LastHeartbeat
}

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

func (t *AgentTunnel) CloseWithReason(reason string) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.State = "closed"
	t.DisconnectErr = cmp.Or(reason, t.DisconnectErr)
	t.mu.Unlock()
	t.closeOnce.Do(func() { close(t.done) })
	t.mux.close()
	if t.Conn == nil {
		return nil
	}
	return t.Conn.Close()
}

func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{
		tunnels:    concurrency.NewStateMap[string, *AgentTunnel](),
		history:    concurrency.NewStateMap[string, TunnelHistory](),
		registered: concurrency.NewSignal[string](),
	}
}

func (r *TunnelRegistry) Active(envID string) mo.Option[*AgentTunnel] {
	if tunnel, _ := r.Get(envID).Get(); tunnel != nil && tunnel.Conn != nil && !tunnel.Conn.IsClosed() {
		return mo.Some(tunnel)
	}
	return mo.None[*AgentTunnel]()
}

func (r *TunnelRegistry) WaitForActive(ctx context.Context, envID string, opts WaitForActiveOptions) mo.Option[*AgentTunnel] {
	if tunnel, ok := r.Active(envID).Get(); ok && tunnel != opts.Except {
		return mo.Some(tunnel)
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
	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	for {
		if tunnel, ok := r.Active(envID).Get(); ok && tunnel != opts.Except {
			return mo.Some(tunnel)
		}
		select {
		case <-waitCtx.Done():
			return mo.None[*AgentTunnel]()
		case <-registered:
		}
	}
}

func (r *TunnelRegistry) Get(envID string) mo.Option[*AgentTunnel] {
	tunnel, ok := r.tunnels.Get(envID)
	return mo.TupleToOption(tunnel, ok)
}

func (r *TunnelRegistry) History(envID string) mo.Option[TunnelHistory] {
	history, ok := r.history.Get(envID)
	return mo.TupleToOption(history, ok)
}

func (r *TunnelRegistry) RuntimeState(envID string) mo.Option[*TunnelRuntimeState] {
	tunnel, ok := r.Active(envID).Get()
	if !ok {
		return mo.None[*TunnelRuntimeState]()
	}
	meta := tunnel.MetadataSnapshot()
	rtt := time.Duration(tunnel.mux.rtt.Load())
	rttCount := tunnel.mux.rttCount.Load()
	bytesIn, bytesOut := tunnel.mux.traffic()
	openStreams := 0
	if session := tunnel.mux.session.Load(); session != nil {
		openStreams = session.NumStreams()
	}
	return mo.Some(&TunnelRuntimeState{
		RoundTripTime: kit.Ternary(rtt > 0, &rtt, nil),
		RTTMin:        kit.Ternary(rttCount > 0, new(time.Duration(tunnel.mux.rttMin.Load())), nil),
		RTTAvg:        kit.Ternary(rttCount > 0, new(time.Duration(tunnel.mux.rttSum.Load()/max(rttCount, 1))), nil),
		RTTMax:        kit.Ternary(rttCount > 0, new(time.Duration(tunnel.mux.rttMax.Load())), nil),
		OpenStreams:   openStreams,
		BytesIn:       bytesIn,
		BytesOut:      bytesOut,
		StreamsOpened: tunnel.mux.opened.Load(),
		Host:          meta.Host,
		RemoteAddr:    meta.RemoteAddr,
		Transport:     tunnel.Conn.Transport(),
		ConnectedAt:   new(meta.ConnectedAt),
		LastHeartbeat: new(meta.LastHeartbeat),
		SessionID:     meta.SessionID,
		AgentInstance: meta.AgentInstance,
		SecurityMode:  meta.SecurityMode,
		Capabilities:  meta.Capabilities,
		State:         meta.State,
	})
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

func (r *TunnelRegistry) RegisterSession(ctx context.Context, tunnel *AgentTunnel) (RegisterResult, error) {
	envID := tunnel.EnvironmentID
	if envID == "" {
		return RegisterResult{Reason: "environment ID is required"}, nil
	}
	if existing, ok := r.Active(envID).Get(); ok && existing != tunnel {
		if session := existing.mux.session.Load(); session != nil {
			if _, pingErr := pingMuxSession(ctx, session, muxRegistrationPingTimeout); pingErr != nil && ctx.Err() == nil {
				slog.InfoContext(ctx, "Existing agent tunnel session failed a liveness ping; replacing it", "environmentId", envID)
				_ = existing.CloseWithReason("agent tunnel liveness ping failed")
			}
		}
	}

	result, err := r.tunnels.ApplyTyped(ctx, "register agent tunnel session", func(tunnels map[string]*AgentTunnel) (registerSessionResult, bool, error) {
		existing := tunnels[envID]
		existingMetadata := existing.MetadataSnapshot()
		sameAgentInstance := existingMetadata.AgentInstance != "" && existingMetadata.AgentInstance == tunnel.MetadataSnapshot().AgentInstance
		switch {
		case r.stopped.Load():
			return registerSessionResult{}, false, errors.New("agent tunnel registry stopped")
		case existing == tunnel:
			return registerSessionResult{Accepted: true}, false, nil
		case existing != nil && existing.Conn != nil && !existing.Conn.IsClosed() &&
			time.Since(existingMetadata.LastHeartbeat) <= TunnelStaleTimeout && !sameAgentInstance:
			return registerSessionResult{Reason: "another agent session is already active"}, false, nil
		}

		tunnels[envID] = tunnel
		return registerSessionResult{
			Accepted:      true,
			DrainPrevious: sameAgentInstance,
			previous:      existing,
			stored:        true,
		}, true, nil
	})
	if err != nil {
		return RegisterResult{}, err
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
	return result.RegisterResult, nil
}

func (r *TunnelRegistry) Unregister(envID string) {
	ctx := context.Background() //nolint:forbidigo // Legacy unregistration path runs outside any request context.
	tunnel, removed, err := r.tunnels.Remove(ctx, "unregister agent tunnel", envID)
	switch {
	case err != nil:
		slog.ErrorContext(ctx, "Failed to unregister agent tunnel", "environmentId", envID, "error", err)
	case removed:
		_ = tunnel.CloseWithReason("")
		r.recordDisconnect(ctx, tunnel)
		slog.InfoContext(ctx, "Agent tunnel unregistered")
	}
}

func (r *TunnelRegistry) UnregisterCurrent(ctx context.Context, current *AgentTunnel) bool {
	envID := current.EnvironmentID
	removed, err := r.tunnels.ApplyTyped(ctx, "unregister current agent tunnel", func(tunnels map[string]*AgentTunnel) (bool, bool, error) {
		if tunnels[envID] != current {
			return false, false, nil
		}
		delete(tunnels, envID)
		return true, true, nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to unregister current agent tunnel", "environmentId", envID, "error", err)
		return false
	}
	if !removed {
		return false
	}

	_ = current.CloseWithReason("")
	r.recordDisconnect(ctx, current)
	slog.InfoContext(ctx, "Agent tunnel unregistered", "environmentId", envID, "sessionId", current.MetadataSnapshot().SessionID)
	return true
}

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
		err = errors.Join(err, tunnel.CloseWithReason("agent tunnel registry stopped"))
	}
	return err
}

func (r *TunnelRegistry) ObserveAgents(meter metric.Meter) (metric.Registration, error) {
	gauge, gaugeErr := meter.Int64ObservableGauge("arcane.agents",
		metric.WithDescription("Connected agents by transport"),
		metric.WithUnit("{agent}"),
	)
	traffic, trafficErr := meter.Int64ObservableCounter("arcane.agent.tunnel.io",
		metric.WithDescription("Bytes the manager received from and transmitted to an agent in its current tunnel session"),
		metric.WithUnit("By"),
	)
	rtt, rttErr := meter.Float64ObservableGauge("arcane.agent.tunnel.rtt",
		metric.WithDescription("Last keepalive round trip to an agent"),
		metric.WithUnit("s"),
	)
	streams, streamsErr := meter.Int64ObservableGauge("arcane.agent.tunnel.streams",
		metric.WithDescription("Open multiplexed streams on an agent tunnel"),
		metric.WithUnit("{stream}"),
	)
	if err := errors.Join(gaugeErr, trafficErr, rttErr, streamsErr); err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		counts := map[string]int64{TransportWebSocket: 0, TransportGRPC: 0}
		for _, tunnel := range r.tunnels.Values() {
			if tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
				continue
			}
			counts[tunnel.Conn.Transport()]++
			tunnelAttrs := metric.WithAttributes(
				attribute.String("arcane.environment.id", tunnel.EnvironmentID),
				attribute.String("host.name", kit.FromPtr(tunnel.MetadataSnapshot().Host).Hostname),
				attribute.String("arcane.agent.transport", tunnel.Conn.Transport()),
			)
			received, transmitted := tunnel.mux.traffic()
			o.ObserveInt64(traffic, received, tunnelAttrs, metric.WithAttributes(attribute.String("network.io.direction", "receive")))
			o.ObserveInt64(traffic, transmitted, tunnelAttrs, metric.WithAttributes(attribute.String("network.io.direction", "transmit")))
			if last := tunnel.mux.rtt.Load(); last > 0 {
				o.ObserveFloat64(rtt, time.Duration(last).Seconds(), tunnelAttrs)
			}
			if session := tunnel.mux.session.Load(); session != nil {
				o.ObserveInt64(streams, int64(session.NumStreams()), tunnelAttrs)
			}
		}
		for transport, count := range counts {
			o.ObserveInt64(gauge, count, metric.WithAttributes(attribute.String("arcane.agent.transport", transport)))
		}
		return nil
	}, gauge, traffic, rtt, streams)
}
