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
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
