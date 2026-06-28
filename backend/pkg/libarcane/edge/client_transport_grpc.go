package edge

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

func (c *TunnelClient) connectAndServeGRPC(ctx context.Context) error {
	managerAddr := strings.TrimSpace(c.managerGRPCAddr)
	if managerAddr == "" {
		return errors.New("manager gRPC address is empty")
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
		tlsConfig, err := buildManagerClientTLSConfigInternal(c.cfg)
		if err != nil {
			return fmt.Errorf("failed to configure edge gRPC TLS: %w", err)
		}
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	slog.DebugContext(ctx, "Dialing manager for gRPC edge tunnel", "addr", managerAddr)

	conn, err := grpc.NewClient(managerAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("failed to dial manager gRPC endpoint: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := c.waitForGRPCReadyInternal(ctx, conn); err != nil {
		return fmt.Errorf("manager gRPC endpoint is not ready: %w", err)
	}

	streamCtx, streamCancel := context.WithCancel(metadata.NewOutgoingContext(ctx, metadata.Pairs(
		strings.ToLower(HeaderAgentToken), c.cfg.AgentToken,
		strings.ToLower(HeaderAPIKey), c.cfg.AgentToken,
		strings.ToLower(HeaderAuthorization), "Bearer "+c.cfg.AgentToken,
	)))
	defer streamCancel()

	method := c.grpcConnectMethodInternal()
	stream, err := c.openTunnelConnectStreamInternal(streamCtx, conn, method)
	if err != nil {
		return fmt.Errorf("failed to open tunnel stream: %w", err)
	}

	tunnelConn := NewGRPCAgentTunnelConn(stream, streamCancel)
	c.setConn(tunnelConn)
	setActiveAgentTunnelConn(tunnelConn)
	defer clearActiveAgentTunnelConn(tunnelConn)
	if err := tunnelConn.Send(c.registerMessageInternal()); err != nil {
		return fmt.Errorf("failed to send register message: %w", err)
	}

	registerMsg, err := c.awaitGRPCRegistrationInternal(ctx)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Edge gRPC tunnel connected to manager",
		"manager_addr", c.managerGRPCAddr,
		"environment_id", registerMsg.EnvironmentID,
	)
	c.markTransportConnectedInternal(EdgeTransportGRPC)

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go c.heartbeatLoop(connCtx)

	return c.messageLoop(connCtx)
}

func (c *TunnelClient) waitForGRPCReadyInternal(ctx context.Context, conn *grpc.ClientConn) error {
	if conn == nil {
		return errors.New("manager gRPC connection is not initialized")
	}

	timeout := c.grpcRegistrationTimeoutInternal()
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Idle {
			conn.Connect()
		}

		if !conn.WaitForStateChange(readyCtx, state) {
			if errors.Is(readyCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timed out waiting for manager gRPC endpoint after %s", timeout)
			}
			return readyCtx.Err()
		}
	}
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

func (c *TunnelClient) awaitGRPCRegistrationInternal(ctx context.Context) (*TunnelMessage, error) {
	return c.awaitRegistrationInternal(ctx)
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
