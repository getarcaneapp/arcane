package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

func TestNormalizeTransport(t *testing.T) {
	assert.Equal(t, TransportAuto, NormalizeTransport(""))
	assert.Equal(t, TransportAuto, NormalizeTransport("auto"))
	assert.Equal(t, TransportAuto, NormalizeTransport("AUTO"))
	assert.Equal(t, TransportGRPC, NormalizeTransport("grpc"))
	assert.Equal(t, TransportGRPC, NormalizeTransport("GRPC"))
	assert.Equal(t, TransportWebSocket, NormalizeTransport("websocket"))
	assert.Equal(t, TransportAuto, NormalizeTransport("invalid"))
}

func TestNormalizeMTLSMode(t *testing.T) {
	assert.Equal(t, MTLSModeDisabled, NormalizeMTLSMode(""))
	assert.Equal(t, MTLSModeOptional, NormalizeMTLSMode("optional"))
	assert.Equal(t, MTLSModeRequired, NormalizeMTLSMode("REQUIRED"))
	assert.Equal(t, MTLSModeDisabled, NormalizeMTLSMode("invalid"))
}

func TestTunnelRegistryRuntimeState(t *testing.T) {
	tunnels := NewTunnelRegistry()
	t.Run("returns false when tunnel is missing", func(t *testing.T) {
		state, ok := tunnels.RuntimeState("env-missing-runtime").Get()
		assert.False(t, ok)
		assert.Nil(t, state)
	})

	t.Run("returns runtime metadata for active tunnel", func(t *testing.T) {
		envID := "env-runtime-live"
		defer tunnels.Unregister(envID)

		registerTestTunnel(t, tunnels, NewAgentTunnel(AgentTunnelConfig{
			Conn:          NewGRPCManagerTunnelConn(nil),
			EnvironmentID: envID,
			SessionID:     "session-1",
			AgentInstance: "agent-1",
			SecurityMode:  "mtls",
			Capabilities:  []string{"container.list"},
		}))

		state, ok := tunnels.RuntimeState(envID).Get()
		assert.True(t, ok)
		if assert.NotNil(t, state) {
			assert.Equal(t, TransportGRPC, state.Transport)
			assert.NotNil(t, state.ConnectedAt)
			assert.NotNil(t, state.LastHeartbeat)
			assert.Equal(t, "session-1", state.SessionID)
			assert.Equal(t, "agent-1", state.AgentInstance)
			assert.Equal(t, "mtls", state.SecurityMode)
			assert.Equal(t, []string{"container.list"}, state.Capabilities)
		}
	})

	t.Run("returns host and tunnel stats for active tunnel", func(t *testing.T) {
		envID := "env-runtime-stats"
		defer tunnels.Unregister(envID)

		tunnel := NewAgentTunnel(AgentTunnelConfig{
			Conn:          NewGRPCManagerTunnelConn(nil),
			Host:          &AgentHostInfo{Hostname: "edge-1", CPUs: 4},
			EnvironmentID: envID,
			RemoteAddr:    "203.0.113.7",
		})
		tunnel.mux.recordRTT(10 * time.Millisecond)
		tunnel.mux.recordRTT(30 * time.Millisecond)
		tunnel.mux.opened.Add(2)
		registerTestTunnel(t, tunnels, tunnel)

		state, ok := tunnels.RuntimeState(envID).Get()
		require.True(t, ok)
		assert.Equal(t, "203.0.113.7", state.RemoteAddr)
		assert.Equal(t, tunnel.Host, state.Host)
		assert.Equal(t, int64(2), state.StreamsOpened)
		assert.Zero(t, state.BytesIn)
		assert.Zero(t, state.BytesOut)
		require.NotNil(t, state.RTTMin)
		require.NotNil(t, state.RTTAvg)
		require.NotNil(t, state.RTTMax)
		assert.Equal(t, 10*time.Millisecond, *state.RTTMin)
		assert.Equal(t, 20*time.Millisecond, *state.RTTAvg)
		assert.Equal(t, 30*time.Millisecond, *state.RTTMax)
	})

	t.Run("omits round trip stats before the first ping", func(t *testing.T) {
		envID := "env-runtime-no-ping"
		defer tunnels.Unregister(envID)

		registerTestTunnel(t, tunnels, NewAgentTunnel(AgentTunnelConfig{EnvironmentID: envID, Conn: NewGRPCManagerTunnelConn(nil)}))

		state, ok := tunnels.RuntimeState(envID).Get()
		require.True(t, ok)
		assert.Nil(t, state.RTTMin)
		assert.Nil(t, state.RTTAvg)
		assert.Nil(t, state.RTTMax)
	})

	t.Run("returns false for closed tunnel", func(t *testing.T) {
		envID := "env-runtime-closed"
		defer tunnels.Unregister(envID)

		tunnel := NewAgentTunnel(AgentTunnelConfig{EnvironmentID: envID, Conn: NewGRPCManagerTunnelConn(nil)})
		_ = tunnel.Conn.Close()
		registerTestTunnel(t, tunnels, tunnel)

		state, ok := tunnels.RuntimeState(envID).Get()
		assert.False(t, ok)
		assert.Nil(t, state)
	})
}

func BenchmarkAgentTunnelProxyRequest(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	payloadSizes := []int{64, 1024, 4096, 64 << 10, 1 << 20, 16 << 20}
	for _, payloadSize := range payloadSizes {
		b.Run(fmt.Sprintf("grpc_payload_%d", payloadSize), func(b *testing.B) {
			tunnels, envID, cleanup := setupGRPCBenchmarkTunnel(b, payloadSize)
			defer cleanup()
			runProxyRequestBenchmark(b, tunnels, envID, payloadSize)
		})

		b.Run(fmt.Sprintf("websocket_payload_%d", payloadSize), func(b *testing.B) {
			tunnels, envID, cleanup := setupWebSocketBenchmarkTunnel(b, payloadSize)
			defer cleanup()
			runProxyRequestBenchmark(b, tunnels, envID, payloadSize)
		})
	}

	for _, payloadSize := range []int{1024, 1 << 20} {
		b.Run(fmt.Sprintf("grpc_parallel_payload_%d", payloadSize), func(b *testing.B) {
			tunnels, envID, cleanup := setupGRPCBenchmarkTunnel(b, payloadSize)
			defer cleanup()
			runParallelProxyRequestBenchmark(b, tunnels, envID, payloadSize)
		})

		b.Run(fmt.Sprintf("websocket_parallel_payload_%d", payloadSize), func(b *testing.B) {
			tunnels, envID, cleanup := setupWebSocketBenchmarkTunnel(b, payloadSize)
			defer cleanup()
			runParallelProxyRequestBenchmark(b, tunnels, envID, payloadSize)
		})
	}
}

func runParallelProxyRequestBenchmark(b *testing.B, tunnels *TunnelRegistry, envID string, payloadSize int) {
	b.Helper()

	ctx := b.Context()
	req := RemoteRequest{
		Headers:       http.Header{"Content-Type": {"application/octet-stream"}},
		EnvironmentID: envID,
		Method:        http.MethodPost,
		Path:          "/api/environments/0/images/upload",
		Body:          make([]byte, payloadSize),
	}

	b.ReportAllocs()
	b.SetBytes(int64(payloadSize))
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := tunnels.Do(ctx, req)
			if err != nil || resp.StatusCode != http.StatusOK || len(resp.Body) != payloadSize {
				b.Errorf("proxy request failed: response=%v err=%v", resp, err)
				return
			}
		}
	})
}

func runProxyRequestBenchmark(b *testing.B, tunnels *TunnelRegistry, envID string, payloadSize int) {
	b.Helper()

	ctx := b.Context()
	req := RemoteRequest{
		Headers:       http.Header{"Content-Type": {"application/octet-stream"}},
		EnvironmentID: envID,
		Method:        http.MethodPost,
		Path:          "/api/environments/0/images/upload",
		Body:          make([]byte, payloadSize),
	}

	b.ReportAllocs()
	b.SetBytes(int64(payloadSize))
	b.ResetTimer()

	for b.Loop() {
		resp, err := tunnels.Do(ctx, req)
		require.NoError(b, err, "proxy request failed: %v", err)
		require.Equal(b, http.StatusOK, resp.StatusCode, "unexpected status code: %d", resp.StatusCode)
		require.Len(b, resp.Body, payloadSize, "unexpected response length: got %d want %d", len(resp.Body), payloadSize)
	}
}

func setupGRPCBenchmarkTunnel(b *testing.B, payloadSize int) (*TunnelRegistry, string, func()) {
	b.Helper()
	tunnels := NewTunnelRegistry()

	ctx, cancel := context.WithCancel(b.Context())
	envID := fmt.Sprintf("bench-grpc-%d", time.Now().UnixNano())

	lis, grpcServer := startTestGRPCTunnelServer(tunnels, envID)
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		cancel()
		require.FailNowf(b, "unexpected failure", "failed to create gRPC client: %v", err)
	}

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(testGRPCOutgoingContext(ctx, "valid-token"))
	if err != nil {
		_ = conn.Close()
		cancel()
		require.FailNowf(b, "unexpected failure", "failed to open gRPC stream: %v", err)
	}

	if sendErr := stream.Send(tunnelpb.AgentMessage_builder{
		Register: tunnelpb.RegisterRequest_builder{
			AgentToken:   "valid-token",
			Capabilities: []string{tunnelCapabilityMux},
		}.Build(),
	}.Build()); sendErr != nil {
		_ = conn.Close()
		cancel()
		require.FailNowf(b, "unexpected failure", "failed to send register message: %v", sendErr)
	}

	if _, recvErr := stream.Recv(); recvErr != nil {
		_ = conn.Close()
		cancel()
		require.FailNowf(b, "unexpected failure", "failed to receive register response: %v", recvErr)
	}

	responseBody := make([]byte, payloadSize)
	agent := &TunnelClient{streamWindowMiB: testStreamWindowMiB, handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(responseBody)
	})}
	agentConn := NewGRPCAgentTunnelConn(stream, func() {})
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		_ = agent.serveMuxSession(ctx, agentConn.muxConn(ctx), testStreamWindowMiB())
	}()

	waitForBenchmarkTunnel(b, tunnels, envID)

	return tunnels, envID, func() {
		cancel()
		<-agentDone
		_ = stream.CloseSend()
		_ = conn.Close()
		grpcServer.GracefulStop()
		tunnels.Unregister(envID)
	}
}

func setupWebSocketBenchmarkTunnel(b *testing.B, payloadSize int) (*TunnelRegistry, string, func()) {
	b.Helper()

	responseBody := make([]byte, payloadSize)
	agent := &TunnelClient{streamWindowMiB: testStreamWindowMiB, handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(responseBody)
	})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = agent.serveMuxSession(r.Context(), NewWebSocketTunnelConn(conn).muxConn(r.Context()), testStreamWindowMiB())
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(b.Context(), wsURL, nil)
	if err != nil {
		server.Close()
		require.FailNowf(b, "unexpected failure", "failed to dial websocket server: %v", err)
	}

	ctx, cancel := context.WithCancel(b.Context())
	tunnel := NewAgentTunnel(AgentTunnelConfig{EnvironmentID: "bench-websocket", Conn: NewWebSocketTunnelConn(conn)})
	tunnels := NewTunnelRegistry()
	registerTestTunnel(b, tunnels, tunnel)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = (&TunnelServer{streamWindowMiB: testStreamWindowMiB}).serveMuxTunnel(ctx, tunnel, tunnel.Conn.muxConn(ctx))
	}()

	return tunnels, tunnel.EnvironmentID, func() {
		cancel()
		_ = tunnel.CloseWithReason("")
		<-serveDone
		server.Close()
	}
}

func waitForBenchmarkTunnel(b *testing.B, tunnels *TunnelRegistry, envID string) {
	b.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tunnels.Active(envID).IsPresent() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.FailNowf(b, "unexpected failure", "timed out waiting for tunnel registration for env %s", envID)
}
