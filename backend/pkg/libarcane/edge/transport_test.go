package edge

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNormalizeEdgeTransport(t *testing.T) {
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport(""))
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport("auto"))
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport("AUTO"))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("grpc"))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("GRPC"))
	assert.Equal(t, EdgeTransportPoll, NormalizeEdgeTransport("poll"))
	assert.Equal(t, EdgeTransportWebSocket, NormalizeEdgeTransport("websocket"))
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport("invalid"))
}

func TestNormalizeEdgeMTLSMode(t *testing.T) {
	assert.Equal(t, EdgeMTLSModeDisabled, NormalizeEdgeMTLSMode(""))
	assert.Equal(t, EdgeMTLSModeOptional, NormalizeEdgeMTLSMode("optional"))
	assert.Equal(t, EdgeMTLSModeRequired, NormalizeEdgeMTLSMode("REQUIRED"))
	assert.Equal(t, EdgeMTLSModeDisabled, NormalizeEdgeMTLSMode("invalid"))
}

func TestUseGRPCEdgeTransport(t *testing.T) {
	assert.False(t, UseGRPCEdgeTransport(nil))
	assert.True(t, UseGRPCEdgeTransport(&Config{EdgeTransport: EdgeTransportAuto}))
	assert.True(t, UseGRPCEdgeTransport(&Config{EdgeTransport: "grpc"}))
	assert.True(t, UseGRPCEdgeTransport(&Config{EdgeTransport: ""}))
	assert.False(t, UseGRPCEdgeTransport(&Config{EdgeTransport: "websocket"}))
}

func TestUseWebSocketEdgeTransport(t *testing.T) {
	assert.False(t, UseWebSocketEdgeTransport(nil))
	assert.True(t, UseWebSocketEdgeTransport(&Config{EdgeTransport: EdgeTransportAuto}))
	assert.True(t, UseWebSocketEdgeTransport(&Config{EdgeTransport: ""}))
	assert.False(t, UseWebSocketEdgeTransport(&Config{EdgeTransport: "grpc"}))
	assert.False(t, UseWebSocketEdgeTransport(&Config{EdgeTransport: "poll"}))
	assert.True(t, UseWebSocketEdgeTransport(&Config{EdgeTransport: "websocket"}))
}

func TestUsePollEdgeTransport(t *testing.T) {
	assert.False(t, UsePollEdgeTransport(nil))
	assert.False(t, UsePollEdgeTransport(&Config{EdgeTransport: ""}))
	assert.False(t, UsePollEdgeTransport(&Config{EdgeTransport: "grpc"}))
	assert.False(t, UsePollEdgeTransport(&Config{EdgeTransport: "websocket"}))
	assert.True(t, UsePollEdgeTransport(&Config{EdgeTransport: "poll"}))
}

func TestGetActiveTunnelTransport(t *testing.T) {
	t.Run("returns false when tunnel is missing", func(t *testing.T) {
		transport, ok := GetActiveTunnelTransport("env-missing")
		assert.False(t, ok)
		assert.Equal(t, "", transport)
	})

	t.Run("detects grpc tunnel", func(t *testing.T) {
		envID := "env-transport-grpc"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(nil))
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.True(t, ok)
		assert.Equal(t, EdgeTransportGRPC, transport)
	})

	t.Run("detects websocket tunnel", func(t *testing.T) {
		envID := "env-transport-ws"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		conn := createTestConn(t)
		defer func() { _ = conn.Close() }()

		tunnel := newWebSocketAgentTunnel(envID, conn)
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.True(t, ok)
		assert.Equal(t, EdgeTransportWebSocket, transport)
	})

	t.Run("returns false for closed tunnel", func(t *testing.T) {
		envID := "env-transport-closed"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(nil))
		_ = tunnel.Conn.Close()
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.False(t, ok)
		assert.Equal(t, "", transport)
	})

	t.Run("returns false for unknown transport implementation", func(t *testing.T) {
		envID := "env-transport-unknown"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, &unknownTunnelConn{})
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.False(t, ok)
		assert.Equal(t, "", transport)
	})
}

func TestGetTunnelRuntimeState(t *testing.T) {
	t.Run("returns false when tunnel is missing", func(t *testing.T) {
		state, ok := GetTunnelRuntimeState("env-missing-runtime")
		assert.False(t, ok)
		assert.Nil(t, state)
	})

	t.Run("returns runtime metadata for active tunnel", func(t *testing.T) {
		envID := "env-runtime-live"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(nil))
		tunnel.SessionID = "session-1"
		tunnel.AgentInstance = "agent-1"
		tunnel.SecurityMode = "mtls"
		tunnel.Capabilities = []string{"container.list"}
		GetRegistry().Register(envID, tunnel)

		state, ok := GetTunnelRuntimeState(envID)
		assert.True(t, ok)
		if assert.NotNil(t, state) {
			assert.Equal(t, EdgeTransportGRPC, state.Transport)
			assert.NotNil(t, state.ConnectedAt)
			assert.NotNil(t, state.LastHeartbeat)
			assert.Equal(t, "session-1", state.SessionID)
			assert.Equal(t, "agent-1", state.AgentInstance)
			assert.Equal(t, "mtls", state.SecurityMode)
			assert.Equal(t, []string{"container.list"}, state.Capabilities)
		}
	})

	t.Run("returns false for closed tunnel", func(t *testing.T) {
		envID := "env-runtime-closed"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(nil))
		_ = tunnel.Conn.Close()
		GetRegistry().Register(envID, tunnel)

		state, ok := GetTunnelRuntimeState(envID)
		assert.False(t, ok)
		assert.Nil(t, state)
	})
}

type unknownTunnelConn struct{}

func (u *unknownTunnelConn) Send(msg *TunnelMessage) error { return nil }

func (u *unknownTunnelConn) Receive() (*TunnelMessage, error) { return nil, nil }

func (u *unknownTunnelConn) IsExpectedReceiveError(error) bool { return false }

func (u *unknownTunnelConn) Close() error { return nil }

func (u *unknownTunnelConn) IsClosed() bool { return false }

func (u *unknownTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return nil, nil
}

func BenchmarkEdgeTunnelProxyRequest(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	payloadSizes := []int{64, 1024, 4096}
	for _, payloadSize := range payloadSizes {
		b.Run(fmt.Sprintf("grpc_payload_%d", payloadSize), func(b *testing.B) {
			tunnel, cleanup := setupGRPCBenchmarkTunnel(b, payloadSize)
			defer cleanup()
			runProxyRequestBenchmark(b, tunnel, payloadSize)
		})

		b.Run(fmt.Sprintf("websocket_payload_%d", payloadSize), func(b *testing.B) {
			tunnel, cleanup := setupWebSocketBenchmarkTunnel(b, payloadSize)
			defer cleanup()
			runProxyRequestBenchmark(b, tunnel, payloadSize)
		})
	}
}

func runProxyRequestBenchmark(b *testing.B, tunnel *AgentTunnel, payloadSize int) {
	b.Helper()

	ctx := context.Background()
	body := make([]byte, payloadSize)
	headers := map[string]string{
		"Content-Type": "application/octet-stream",
	}

	b.ReportAllocs()
	b.SetBytes(int64(payloadSize))
	b.ResetTimer()

	for b.Loop() {
		statusCode, _, respBody, err := ProxyRequest(ctx, tunnel, http.MethodPost, "/api/bench", "", headers, body)
		if err != nil {
			b.Fatalf("proxy request failed: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("unexpected status code: %d", statusCode)
		}
		if len(respBody) != payloadSize {
			b.Fatalf("unexpected response length: got %d want %d", len(respBody), payloadSize)
		}
	}
}

func setupGRPCBenchmarkTunnel(b *testing.B, payloadSize int) (*AgentTunnel, func()) {
	b.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	envID := fmt.Sprintf("bench-grpc-%d", time.Now().UnixNano())
	GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
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
		tunnelServer.WaitForCleanupDone()
		b.Fatalf("failed to create gRPC client: %v", err)
	}

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		_ = conn.Close()
		cancel()
		tunnelServer.WaitForCleanupDone()
		b.Fatalf("failed to open gRPC stream: %v", err)
	}

	if err := stream.Send(&tunnelpb.AgentMessage{
		Payload: &tunnelpb.AgentMessage_Register{
			Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"},
		},
	}); err != nil {
		_ = conn.Close()
		cancel()
		tunnelServer.WaitForCleanupDone()
		b.Fatalf("failed to send register message: %v", err)
	}

	if _, err := stream.Recv(); err != nil {
		_ = conn.Close()
		cancel()
		tunnelServer.WaitForCleanupDone()
		b.Fatalf("failed to receive register response: %v", err)
	}

	responseBody := make([]byte, payloadSize)
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			req := msg.GetHttpRequest()
			if req == nil {
				continue
			}
			if err := stream.Send(&tunnelpb.AgentMessage{
				Payload: &tunnelpb.AgentMessage_HttpResponse{
					HttpResponse: &tunnelpb.HttpResponse{
						RequestId: req.GetRequestId(),
						Status:    http.StatusOK,
						Body:      responseBody,
					},
				},
			}); err != nil {
				return
			}
		}
	}()

	tunnel := waitForBenchmarkTunnel(b, envID)

	return tunnel, func() {
		_ = conn.Close()
		grpcServer.GracefulStop()
		cancel()
		<-agentDone
		tunnelServer.WaitForCleanupDone()
		GetRegistry().Unregister(envID)
	}
}

func setupWebSocketBenchmarkTunnel(b *testing.B, payloadSize int) (*AgentTunnel, func()) {
	b.Helper()

	responseBody := make([]byte, payloadSize)
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		for {
			var msg TunnelMessage
			if err := conn.ReadJSON(&msg); err != nil {
				_ = conn.Close()
				return
			}

			if msg.Type != MessageTypeRequest {
				continue
			}

			resp := TunnelMessage{
				ID:     msg.ID,
				Type:   MessageTypeResponse,
				Status: http.StatusOK,
				Body:   responseBody,
			}
			if err := conn.WriteJSON(resp); err != nil {
				_ = conn.Close()
				return
			}
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		b.Fatalf("failed to dial websocket server: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	tunnel := NewAgentTunnelWithConn("bench-websocket", NewTunnelConn(conn))
	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		for {
			msg, err := tunnel.Conn.Receive()
			if err != nil {
				return
			}
			if req, ok := tunnel.Pending.Load(msg.ID); ok {
				pendingReq := req.(*PendingRequest)
				pendingReq.ResponseCh <- msg
			}
		}
	}()

	return tunnel, func() {
		_ = tunnel.Close()
		server.Close()
		<-dispatchDone
	}
}

func waitForBenchmarkTunnel(b *testing.B, envID string) *AgentTunnel {
	b.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tunnel, ok := GetRegistry().Get(envID)
		if ok && tunnel != nil {
			return tunnel
		}
		time.Sleep(10 * time.Millisecond)
	}

	b.Fatalf("timed out waiting for tunnel registration for env %s", envID)
	return nil
}
