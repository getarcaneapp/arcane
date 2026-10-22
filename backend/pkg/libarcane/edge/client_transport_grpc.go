package edge

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v3/proto/tunnel/v1"
)

// tunnelConnectPath is the manager route for both the WebSocket and gRPC tunnel, so reverse proxies need one rule.
const tunnelConnectPath = "/api/tunnel/connect"

func (c *TunnelClient) connectAndServeGRPC(ctx context.Context) error {
	managerAddr := strings.TrimSpace(c.managerGRPCAddr)
	if managerAddr == "" {
		return errors.New("manager gRPC address is empty")
	}

	dialOpts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGRPCTunnelMessageSize)),
		// Larger socket buffers suit the single long-lived tunnel stream; window sizes stay unset so BDP probing tunes them.
		grpc.WithReadBufferSize(256 << 10),
		grpc.WithWriteBufferSize(256 << 10),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1 * time.Second,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   30 * time.Second,
			},
			MinConnectTimeout: 10 * time.Second,
		}),
		// net/http owns HTTP/2 pings while the manager serves gRPC through ServeHTTP; a native
		// grpc.Serve listener would need an enforcement policy permitting this interval.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if managerUsesTLS(c.cfg) {
		tlsConfig, err := buildManagerClientTLSConfig(c.cfg)
		if err != nil {
			return fmt.Errorf("failed to configure edge gRPC TLS: %w", err)
		}
		tlsConfig = cmp.Or(tlsConfig, &tls.Config{MinVersion: tls.VersionTLS12})
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

	readyCtx, cancelReady := context.WithTimeout(ctx, c.registrationTimeout)
	defer cancelReady()
	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		if state == connectivity.Idle {
			conn.Connect()
		}
		if !conn.WaitForStateChange(readyCtx, state) {
			if errors.Is(readyCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("manager gRPC endpoint is not ready: timed out waiting for manager gRPC endpoint after %s", c.registrationTimeout)
			}
			return fmt.Errorf("manager gRPC endpoint is not ready: %w", readyCtx.Err())
		}
	}

	// metadata.New lowercases the keys itself.
	streamCtx, streamCancel := context.WithCancel(metadata.NewOutgoingContext(ctx, metadata.New(agentAuthCredentials(c.cfg.AgentToken))))
	defer streamCancel()

	clientStream, err := conn.NewStream(streamCtx, &tunnelpb.TunnelService_ServiceDesc.Streams[0], tunnelConnectPath, grpc.StaticMethod())
	if err != nil {
		return fmt.Errorf("failed to open tunnel stream: %w", err)
	}
	stream := &grpc.GenericClientStream[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]{ClientStream: clientStream}

	if serveTunnelSessionErr := c.serveTunnelSession(ctx, NewGRPCAgentTunnelConn(stream, streamCancel), managerAddr); serveTunnelSessionErr != nil {
		if errors.Is(serveTunnelSessionErr, errTunnelRegistrationTimeout) {
			// The channel reached Ready, so TCP/TLS works but gRPC framing was never answered end to end.
			return fmt.Errorf("manager accepted the TCP/TLS connection but never answered gRPC tunnel registration; "+
				"if a reverse proxy (Traefik/Pangolin/Nginx) fronts the manager, it is likely not forwarding gRPC (HTTP/2 with trailers) on %s: %w",
				tunnelConnectPath, serveTunnelSessionErr)
		}
		return serveTunnelSessionErr
	}
	return nil
}
