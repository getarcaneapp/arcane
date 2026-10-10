package edge

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

func TestGRPCTunnel_RequestResponse(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
	stream, err := client.Connect(testGRPCOutgoingContext(ctx, "valid-token"))
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
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			agentErrCh <- recvErr
			return
		}

		req := msg.GetCommandRequest()
		if req == nil {
			agentErrCh <- errors.New("expected command request")
			return
		}

		if sendErr := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandAck{CommandAck: &tunnelpb.CommandAck{
			CommandId: req.GetCommandId(),
		}}}); sendErr != nil {
			agentErrCh <- sendErr
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandComplete{CommandComplete: &tunnelpb.CommandComplete{
			CommandId: req.GetCommandId(),
			Status:    200,
			Headers:   map[string]string{"Content-Type": "text/plain"},
			Body:      []byte("ok"),
		}}})
	}()

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID).Get()
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

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
	stream, err := client.Connect(testGRPCOutgoingContext(ctx, "valid-token"))
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())

	agentErrCh := make(chan error, 1)
	go func() {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			agentErrCh <- recvErr
			return
		}

		req := msg.GetCommandRequest()
		if req == nil {
			agentErrCh <- errors.New("expected command request")
			return
		}

		if sendErr := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandAck{CommandAck: &tunnelpb.CommandAck{
			CommandId: req.GetCommandId(),
		}}}); sendErr != nil {
			agentErrCh <- sendErr
			return
		}

		if sendErr2 := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandOutput{CommandOutput: &tunnelpb.CommandOutput{
			CommandId: req.GetCommandId(),
			Data:      []byte("hello "),
			Sequence:  0,
		}}}); sendErr2 != nil {
			agentErrCh <- sendErr2
			return
		}

		if sendErr3 := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandOutput{CommandOutput: &tunnelpb.CommandOutput{
			CommandId: req.GetCommandId(),
			Data:      []byte("world"),
			Sequence:  1,
		}}}); sendErr3 != nil {
			agentErrCh <- sendErr3
			return
		}

		time.Sleep(10 * time.Millisecond)

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandComplete{CommandComplete: &tunnelpb.CommandComplete{
			CommandId: req.GetCommandId(),
			Status:    200,
			Headers:   map[string]string{"Content-Type": "text/plain", "X-Arcane-Tunnel-Stream": "1"},
			Streaming: true,
		}}})
	}()

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID).Get()
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

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
		strings.ToLower(HeaderAgentToken), "  valid-token  ",
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

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
		strings.ToLower(HeaderAPIKey), " valid-token ",
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

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
	stream, err := client.Connect(testGRPCOutgoingContext(ctx, "invalid-token"))
	require.NoError(t, err)

	if sendErr := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "invalid-token"}}}); sendErr != nil {
		require.ErrorIs(t, sendErr, io.EOF)
	}

	_, err = stream.Recv()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "invalid agent token")
}

func TestGRPCTunnel_FirstMessageMustBeRegister(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
	stream, err := client.Connect(testGRPCOutgoingContext(ctx, "valid-token"))
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

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

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
	stream, err := client.Connect(testGRPCOutgoingContext(ctx, "valid-token"))
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
	tunnelServer := NewTunnelServerWithRegistry(GetRegistry(),
		func(ctx context.Context, token string) (string, error) {
			if token != "valid-token" {
				return "", errors.New("invalid token")
			}
			return envID, nil
		},
		nil,
	)
	grpcServer := grpc.NewServer(tunnelServer.GRPCServerOptions()...)
	go tunnelServer.StartCleanupLoop(ctx)
	tunnelpb.RegisterTunnelServiceServer(grpcServer, tunnelServer)
	return lis, grpcServer, tunnelServer
}

func testGRPCOutgoingContext(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(strings.ToLower(HeaderAgentToken), token))
}
