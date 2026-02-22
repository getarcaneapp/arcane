package edge

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCTunnel_RequestResponse(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"}}})
	require.NoError(t, err)

	// First manager message confirms registration.
	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())
	assert.Equal(t, envID, registerResp.GetRegisterResponse().GetEnvironmentId())

	agentErrCh := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			agentErrCh <- err
			return
		}

		req := msg.GetHttpRequest()
		if req == nil {
			agentErrCh <- errors.New("expected http request")
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HttpResponse{HttpResponse: &tunnelpb.HttpResponse{
			RequestId: req.GetRequestId(),
			Status:    200,
			Headers:   map[string]string{"Content-Type": "text/plain"},
			Body:      []byte("ok"),
		}}})
	}()

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID)
		return ok && tunnel != nil
	}, time.Second, 10*time.Millisecond)

	statusCode, headers, body, err := ProxyRequest(ctx, tunnel, "GET", "/api/health", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, "text/plain", headers["Content-Type"])
	assert.Equal(t, "ok", string(body))

	require.NoError(t, <-agentErrCh)
}

func TestGRPCTunnel_RequestResponseStreamingChunks(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-stream-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())

	agentErrCh := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			agentErrCh <- err
			return
		}

		req := msg.GetHttpRequest()
		if req == nil {
			agentErrCh <- errors.New("expected http request")
			return
		}

		if err := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HttpResponse{HttpResponse: &tunnelpb.HttpResponse{
			RequestId: req.GetRequestId(),
			Status:    200,
			Headers:   map[string]string{"Content-Type": "text/plain", "X-Arcane-Tunnel-Stream": "1"},
		}}}); err != nil {
			agentErrCh <- err
			return
		}

		if err := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamData{StreamData: &tunnelpb.StreamData{
			RequestId: req.GetRequestId(),
			Data:      []byte("hello "),
		}}}); err != nil {
			agentErrCh <- err
			return
		}

		if err := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamData{StreamData: &tunnelpb.StreamData{
			RequestId: req.GetRequestId(),
			Data:      []byte("world"),
		}}}); err != nil {
			agentErrCh <- err
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamEnd{StreamEnd: &tunnelpb.StreamEnd{
			RequestId: req.GetRequestId(),
		}}})
	}()

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID)
		return ok && tunnel != nil
	}, time.Second, 10*time.Millisecond)

	statusCode, headers, body, err := ProxyRequest(ctx, tunnel, "GET", "/api/health", "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, "text/plain", headers["Content-Type"])
	assert.Equal(t, "hello world", string(body))
	assert.Empty(t, headers["X-Arcane-Tunnel-Stream"])

	require.NoError(t, <-agentErrCh)
}

func TestGRPCTunnel_MetadataAuthFallback(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-md-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		strings.ToLower(remenv.HeaderAgentToken), "  valid-token  ",
	))
	stream, err := client.Connect(streamCtx)
	require.NoError(t, err)

	// Empty token in register is accepted via metadata fallback.
	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: ""}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())
	assert.Equal(t, envID, registerResp.GetRegisterResponse().GetEnvironmentId())
}

func TestGRPCTunnel_MetadataAPIKeyFallback(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-md-api-key-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		strings.ToLower(remenv.HeaderAPIKey), " valid-token ",
	))
	stream, err := client.Connect(streamCtx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: ""}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())
	assert.Equal(t, envID, registerResp.GetRegisterResponse().GetEnvironmentId())
}

func TestGRPCTunnel_InvalidTokenRejected(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-invalid-token-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "invalid-token"}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.False(t, registerResp.GetRegisterResponse().GetAccepted())
	assert.Equal(t, "invalid agent token", registerResp.GetRegisterResponse().GetError())

	_, err = stream.Recv()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "invalid agent token")
}

func TestGRPCTunnel_FirstMessageMustBeRegister(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-first-msg-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HeartbeatPing{HeartbeatPing: &tunnelpb.HeartbeatPing{}}})
	require.NoError(t, err)

	_, err = stream.Recv()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "first message must be register")
}

func TestGRPCTunnel_RegisterMessageRequiredOnEOF(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-grpc-eof-first-msg-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "register message required")
}

func startTestGRPCTunnelServer(ctx context.Context, envID string) (*bufconn.Listener, *grpc.Server, *TunnelServer) {
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	tunnelServer := NewTunnelServer(
		func(ctx context.Context, token string) (string, error) {
			if token != "valid-token" {
				return "", errors.New("invalid token")
			}
			return envID, nil
		},
		nil,
	)
	go tunnelServer.StartCleanupLoop(ctx)
	tunnelpb.RegisterTunnelServiceServer(grpcServer, tunnelServer)
	return lis, grpcServer, tunnelServer
}
