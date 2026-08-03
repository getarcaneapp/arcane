package edge

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"emperror.dev/errors"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
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
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGRPCTunnelMessageSize)),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1 * time.Second,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   30 * time.Second,
			},
			MinConnectTimeout: 10 * time.Second,
		}),
		// The manager currently serves gRPC through grpc.Server.ServeHTTP, so
		// net/http owns HTTP/2 ping handling. A future native grpc.Serve listener
		// must add an enforcement policy whose MinTime permits this interval.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if c.useTLSForManagerGRPC() {
		tlsConfig, err := buildManagerClientTLSConfigInternal(c.cfg)
		if err != nil {
			return errors.WrapIf(err, "failed to configure edge gRPC TLS")
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
		return errors.WrapIf(err, "failed to dial manager gRPC endpoint")
	}
	defer func() { _ = conn.Close() }()

	if err := c.waitForGRPCReadyInternal(ctx, conn); err != nil {
		return errors.WrapIf(err, "manager gRPC endpoint is not ready")
	}

	// metadata.New lowercases the keys itself.
	streamCtx, streamCancel := context.WithCancel(metadata.NewOutgoingContext(ctx,
		metadata.New(agentAuthCredentialsInternal(c.cfg.AgentToken))))
	defer streamCancel()

	method := c.grpcConnectMethodInternal()
	stream, err := c.openTunnelConnectStreamInternal(streamCtx, conn, method)
	if err != nil {
		return errors.WrapIf(err, "failed to open tunnel stream")
	}

	if err := c.serveTunnelSessionInternal(ctx, NewGRPCAgentTunnelConn(stream, streamCancel), managerAddr); err != nil {
		if errors.Is(err, errTunnelRegistrationTimeout) {
			// The channel already reached Ready, so TCP/TLS works but gRPC
			// framing was never answered end to end.
			return errors.WrapIf(err,
				"manager accepted the TCP/TLS connection but never answered gRPC tunnel registration; "+
					"if a reverse proxy (Traefik/Pangolin/Nginx) fronts the manager, it is likely not forwarding gRPC (HTTP/2 with trailers) on /api/tunnel/connect")
		}
		return err
	}
	return nil
}

func (c *TunnelClient) waitForGRPCReadyInternal(ctx context.Context, conn *grpc.ClientConn) error {
	if conn == nil {
		return errors.New("manager gRPC connection is not initialized")
	}

	timeout := c.registrationTimeoutInternal()
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
				return errors.Errorf("timed out waiting for manager gRPC endpoint after %s", timeout)
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
