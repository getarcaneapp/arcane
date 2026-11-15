package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

type (
	Readiness string

	ConnectionState string
)

const (
	ReadinessReady        Readiness = "ready"
	ReadinessReconnecting Readiness = "reconnecting"
	ReadinessNotReady     Readiness = "not-ready"

	ConnectionStateOnline       ConnectionState = "online"
	ConnectionStateUnresponsive ConnectionState = "unresponsive"
	ConnectionStateOffline      ConnectionState = "offline"

	leaseDuration     = 4 * DefaultHeartbeatInterval
	leaseProbeTimeout = 3 * time.Second
)

var ErrAgentNotResponding = errors.New("agent is not responding")

func (r *TunnelRegistry) GrantLeases(envIDs []string) {
	expiry := time.Now().Add(leaseDuration)
	for _, envID := range envIDs {
		r.leases.Store(envID, expiry)
	}
}

func (r *TunnelRegistry) Readiness(envID string) TunnelReadiness {
	tunnel, connected := r.Active(envID).Get()
	stored, _ := r.leases.Load(envID)
	expiry, _ := stored.(time.Time)
	left := max(time.Until(expiry), 0)
	switch {
	case connected && (left > 0 || !slices.Contains(tunnel.Capabilities, tunnelCapabilityReadinessLease)):
		return TunnelReadiness{
			Tunnel:    tunnel,
			State:     ReadinessReady,
			LeaseLeft: left,
		}
	case !connected && left > 0:
		return TunnelReadiness{
			State:     ReadinessReconnecting,
			LeaseLeft: left,
		}
	default:
		return TunnelReadiness{
			Tunnel: tunnel,
			State:  ReadinessNotReady,
		}
	}
}

func (r *TunnelRegistry) Acquire(ctx context.Context, envID string, opts AcquireOptions) (*AgentTunnel, error) {
	readiness := r.Readiness(envID)
	if opts.Except == nil {
		switch {
		case readiness.State == ReadinessReady:
			return readiness.Tunnel, nil
		case readiness.Tunnel != nil:
			return nil, ErrAgentNotResponding
		case readiness.State == ReadinessNotReady || httpx.IsEnvironmentFanOutRequest(ctx):
			return nil, ErrEnvironmentUnavailable
		}
	}
	tunnel, ok := r.WaitForActive(ctx, envID, WaitForActiveOptions{
		Except:  opts.Except,
		Timeout: kit.Ternary(opts.Except != nil, DefaultTunnelReconnectTimeout, min(readiness.LeaseLeft, DefaultTunnelReconnectTimeout)),
	}).Get()
	if !ok {
		return nil, ErrEnvironmentUnavailable
	}
	return tunnel, nil
}

func (s *TunnelServer) muxLeaseHandler(ctx context.Context, tunnel *AgentTunnel) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		tunnel.leaseMu.Lock()
		defer tunnel.leaseMu.Unlock()
		s.registry.GrantLeases([]string{tunnel.EnvironmentID})
		if tunnel.leaseLapsed {
			tunnel.leaseLapsed = false
			slog.InfoContext(ctx, "Agent resumed renewing its readiness lease", "environmentId", tunnel.EnvironmentID)
			s.updateConnectionStatus(ctx, tunnel, ConnectionStateOnline)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *TunnelServer) checkLease(ctx context.Context, tunnel *AgentTunnel) {
	tunnel.leaseMu.Lock()
	defer tunnel.leaseMu.Unlock()
	readiness := s.registry.Readiness(tunnel.EnvironmentID)
	if readiness.Tunnel == tunnel && readiness.State == ReadinessNotReady && !tunnel.leaseLapsed {
		tunnel.leaseLapsed = true
		slog.WarnContext(ctx, "Agent readiness lease expired", "environmentId", tunnel.EnvironmentID)
		s.updateConnectionStatus(ctx, tunnel, ConnectionStateUnresponsive)
	}
}

func (c *TunnelClient) renewLeases(ctx context.Context, events *http.Client) {
	ticker := time.NewTicker(DefaultHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.renewLease(ctx, events); err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "Agent readiness lease not renewed", "error", err)
			}
		}
	}
}

func (c *TunnelClient) renewLease(ctx context.Context, events *http.Client) error {
	probeCtx, cancelProbe := context.WithTimeout(ctx, leaseProbeTimeout)
	defer cancelProbe()
	if err := c.readinessProbe(probeCtx); err != nil {
		return fmt.Errorf("readiness probe failed: %w", err)
	}
	renewCtx, cancelRenew := context.WithTimeout(ctx, DefaultHeartbeatInterval)
	defer cancelRenew()
	req, err := http.NewRequestWithContext(renewCtx, http.MethodPost, muxManagerBaseURL+muxLeasePath, http.NoBody)
	if err != nil {
		return fmt.Errorf("create lease renewal request: %w", err)
	}
	resp, err := events.Do(req)
	if err != nil {
		return fmt.Errorf("send lease renewal: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("manager rejected lease renewal with status %d", resp.StatusCode)
	}
	return nil
}
