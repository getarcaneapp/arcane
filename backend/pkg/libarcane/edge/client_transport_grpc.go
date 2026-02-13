package edge

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

func (c *TunnelClient) connectAndServeGRPC(ctx context.Context) error {
	managerAddr := strings.TrimSpace(c.managerGRPCAddr)
	if managerAddr == "" {
		return fmt.Errorf("manager gRPC address is empty")
	}

	dialOpts := []grpc.DialOption{
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1 * time.Second,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   30 * time.Second,
			},
			MinConnectTimeout: 10 * time.Second,
		}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if c.useTLSForManagerGRPC() {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{ //nolint:gosec
			MinVersion: tls.VersionTLS12,
		})))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	slog.DebugContext(ctx, "Dialing manager for gRPC edge tunnel", "addr", managerAddr)

	conn, err := grpc.NewClient(managerAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("failed to dial manager gRPC endpoint: %w", err)
	}
	defer func() { _ = conn.Close() }()

	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		strings.ToLower(remenv.HeaderAgentToken), c.cfg.AgentToken,
		strings.ToLower(remenv.HeaderAPIKey), c.cfg.AgentToken,
	))
	method := c.grpcConnectMethodInternal()
	stream, err := c.openTunnelConnectStreamInternal(streamCtx, conn, method)
	if err != nil {
		return fmt.Errorf("failed to open tunnel stream: %w", err)
	}

	c.conn = NewGRPCAgentTunnelConn(stream)
	setActiveAgentTunnelConn(c.conn)
	defer clearActiveAgentTunnelConn(c.conn)
	if err := c.conn.Send(&TunnelMessage{
		Type:       MessageTypeRegister,
		AgentToken: c.cfg.AgentToken,
	}); err != nil {
		return fmt.Errorf("failed to send register message: %w", err)
	}

	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go c.heartbeatLoop(heartbeatCtx)

	return c.messageLoop(ctx)
}

func (c *TunnelClient) openTunnelConnectStreamInternal(
	ctx context.Context,
	conn *grpc.ClientConn,
	method string,
) (grpc.BidiStreamingClient[tunnelpb.AgentMessage, tunnelpb.ManagerMessage], error) {
	stream, err := conn.NewStream(ctx, &tunnelpb.TunnelService_ServiceDesc.Streams[0], method, grpc.StaticMethod())
	if err != nil {
		return nil, err
	}
	return &grpc.GenericClientStream[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]{ClientStream: stream}, nil
}

func (c *TunnelClient) grpcConnectMethodInternal() string {
	return "/api/tunnel/connect"
}

func (c *TunnelClient) useTLSForManagerGRPC() bool {
	baseURL := strings.TrimSpace(c.cfg.GetManagerBaseURL())
	if baseURL == "" {
		return false
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Scheme, "https")
}
