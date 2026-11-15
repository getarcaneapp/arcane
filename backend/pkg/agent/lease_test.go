package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

type (
	leaseScenario struct {
		lease     time.Duration
		capable   bool
		connected bool
		leased    bool
	}
	leaseRoundTripper struct {
		paths  []string
		status int
	}
)

func testReadinessProbe(context.Context) error {
	return nil
}

func newLeaseTestTunnel(t *testing.T, envID string, capable bool) *AgentTunnel {
	t.Helper()
	conn := createTestConn(t)
	t.Cleanup(func() {
		_ = conn.CloseNow()
	})
	capabilities := []string{tunnelCapabilityMux}
	if capable {
		capabilities = append(capabilities, tunnelCapabilityReadinessLease)
	}
	return NewAgentTunnel(AgentTunnelConfig{
		Conn:          NewWebSocketTunnelConn(conn),
		EnvironmentID: envID,
		Capabilities:  capabilities,
	})
}

func setTestLease(t *testing.T, r *TunnelRegistry, envID string, left time.Duration) {
	t.Helper()
	r.leases.Store(envID, time.Now().Add(left))
}

func (s leaseScenario) registry(t *testing.T, envID string) *TunnelRegistry {
	t.Helper()
	r := NewTunnelRegistry()
	if s.connected {
		registerTestTunnel(t, r, newLeaseTestTunnel(t, envID, s.capable))
	}
	if s.leased {
		setTestLease(t, r, envID, s.lease)
	}
	return r
}

func TestTunnelRegistryReadiness(t *testing.T) {
	tests := []struct {
		name     string
		want     Readiness
		scenario leaseScenario
	}{
		{
			name:     "agent without lease capability is ready while connected",
			want:     ReadinessReady,
			scenario: leaseScenario{connected: true},
		},
		{
			name: "lease agent with valid lease is ready",
			want: ReadinessReady,
			scenario: leaseScenario{
				lease:     time.Minute,
				capable:   true,
				connected: true,
				leased:    true,
			},
		},
		{
			name: "lease agent with expired lease is not ready",
			want: ReadinessNotReady,
			scenario: leaseScenario{
				lease:     -time.Second,
				capable:   true,
				connected: true,
				leased:    true,
			},
		},
		{
			name: "disconnected agent inside its lease is reconnecting",
			want: ReadinessReconnecting,
			scenario: leaseScenario{
				lease:  time.Minute,
				leased: true,
			},
		},
		{
			name: "disconnected agent past its lease is not ready",
			want: ReadinessNotReady,
			scenario: leaseScenario{
				lease:  -time.Second,
				leased: true,
			},
		},
		{
			name: "unknown agent is not ready",
			want: ReadinessNotReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.scenario.registry(t, "env-1")
			readiness := r.Readiness("env-1")
			assert.Equal(t, tt.want, readiness.State)
			assert.Equal(t, tt.scenario.connected, readiness.Tunnel != nil)
		})
	}
}

func TestTunnelRegistryAcquire(t *testing.T) {
	tests := []struct {
		wantErr  error
		name     string
		scenario leaseScenario
		fanOut   bool
	}{
		{
			name: "ready agent is returned",
			scenario: leaseScenario{
				lease:     time.Minute,
				capable:   true,
				connected: true,
				leased:    true,
			},
		},
		{
			name: "connected agent with expired lease fails as not responding",
			scenario: leaseScenario{
				lease:     -time.Second,
				capable:   true,
				connected: true,
				leased:    true,
			},
			wantErr: ErrAgentNotResponding,
		},
		{
			name:    "agent without tunnel or lease fails immediately",
			wantErr: ErrEnvironmentUnavailable,
		},
		{
			name: "fan-out request does not wait for a reconnecting agent",
			scenario: leaseScenario{
				lease:  time.Minute,
				leased: true,
			},
			fanOut:  true,
			wantErr: ErrEnvironmentUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.scenario.registry(t, "env-1")
			ctx := t.Context()
			if tt.fanOut {
				ctx = httpx.WithEnvironmentScope(ctx, &httpx.EnvironmentScope{})
			}
			started := time.Now()
			tunnel, err := r.Acquire(ctx, "env-1", AcquireOptions{})
			assert.Less(t, time.Since(started), time.Second)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, tunnel)
				return
			}
			require.NoError(t, err)
			assert.Same(t, r.Active("env-1").MustGet(), tunnel)
		})
	}
}

func TestTunnelRegistryAcquireWaitsOnlyWhileLeaseIsValid(t *testing.T) {
	t.Run("reconnect inside the lease", func(t *testing.T) {
		r := NewTunnelRegistry()
		setTestLease(t, r, "env-1", 5*time.Second)
		tunnel := newLeaseTestTunnel(t, "env-1", true)
		time.AfterFunc(50*time.Millisecond, func() {
			_, _ = r.RegisterSession(t.Context(), tunnel)
		})

		acquired, err := r.Acquire(t.Context(), "env-1", AcquireOptions{})
		require.NoError(t, err)
		assert.Same(t, tunnel, acquired)
		assert.Equal(t, ReadinessReady, r.Readiness("env-1").State)
	})

	t.Run("lease expires before reconnect", func(t *testing.T) {
		r := NewTunnelRegistry()
		setTestLease(t, r, "env-1", 100*time.Millisecond)

		started := time.Now()
		_, err := r.Acquire(t.Context(), "env-1", AcquireOptions{})
		require.ErrorIs(t, err, ErrEnvironmentUnavailable)
		assert.Less(t, time.Since(started), time.Second)
	})
}

func TestTunnelRegistryGrantLeases(t *testing.T) {
	r := NewTunnelRegistry()
	registerTestTunnel(t, r, newLeaseTestTunnel(t, "capable", true))
	registerTestTunnel(t, r, newLeaseTestTunnel(t, "legacy", false))

	r.GrantLeases([]string{"seeded"})

	assert.InDelta(t, leaseDuration, r.Readiness("seeded").LeaseLeft, float64(time.Second))
	assert.InDelta(t, leaseDuration, r.Readiness("capable").LeaseLeft, float64(time.Second))
	_, legacyLeased := r.leases.Load("legacy")
	assert.False(t, legacyLeased)
	assert.Equal(t, 4*DefaultHeartbeatInterval, leaseDuration)
}

func TestTunnelServerLeaseTransitions(t *testing.T) {
	var mu sync.Mutex
	var states []ConnectionState
	r := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		Registry: r,
		StatusCallback: func(_ context.Context, _ string, state ConnectionState) {
			mu.Lock()
			defer mu.Unlock()
			states = append(states, state)
		},
		MTLS: NewMTLS(MTLSConfig{}),
	})
	tunnel := newLeaseTestTunnel(t, "env-1", true)
	registerTestTunnel(t, r, tunnel)

	server.checkLease(t.Context(), tunnel)
	setTestLease(t, r, "env-1", -time.Second)
	server.checkLease(t.Context(), tunnel)
	server.checkLease(t.Context(), tunnel)
	assert.Equal(t, ReadinessNotReady, r.Readiness("env-1").State)

	recorder := httptest.NewRecorder()
	server.muxLeaseHandler(t.Context(), tunnel).ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodPost, muxLeasePath, http.NoBody))
	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, ReadinessReady, r.Readiness("env-1").State)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []ConnectionState{ConnectionStateUnresponsive, ConnectionStateOnline}, states)
}

func (rt *leaseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.paths = append(rt.paths, req.URL.Path)
	return &http.Response{
		StatusCode: rt.status,
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func TestTunnelClientRenewLease(t *testing.T) {
	tests := []struct {
		probeErr  error
		name      string
		wantPaths []string
		status    int
		wantErr   bool
	}{
		{
			name:      "healthy agent renews",
			status:    http.StatusNoContent,
			wantPaths: []string{muxLeasePath},
		},
		{
			name:     "failed probe skips renewal",
			probeErr: errors.New("docker ping failed"),
			status:   http.StatusNoContent,
			wantErr:  true,
		},
		{
			name:      "manager rejection is reported",
			status:    http.StatusNotFound,
			wantPaths: []string{muxLeasePath},
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &leaseRoundTripper{status: tt.status}
			client := &TunnelClient{
				readinessProbe: func(context.Context) error {
					return tt.probeErr
				},
			}
			err := client.renewLease(t.Context(), &http.Client{Transport: transport})
			assert.Equal(t, tt.wantErr, err != nil)
			assert.Equal(t, tt.wantPaths, transport.paths)
		})
	}
}
