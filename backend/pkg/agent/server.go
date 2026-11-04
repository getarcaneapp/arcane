package agent

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/logging"
	wshub "getarcane.dev/arcane/v3/pkg/ws"
	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
	certgen "getarcane.dev/cli/v3/pkg/generate"
)

type (
	EnvironmentResolver func(ctx context.Context, token string) (environmentID string, err error)

	StatusUpdateCallback func(ctx context.Context, environmentID string, connected bool)

	EventCallback func(ctx context.Context, environmentID string, event *TunnelEvent) error
)

const (
	TunnelStaleTimeout = 2 * time.Minute
	// tunnelStaleSweepInterval backs up read-deadline liveness, so it runs well under TunnelStaleTimeout.
	tunnelStaleSweepInterval = time.Minute
)

var observeAgentsOnce sync.Once

func NewTunnelServer(opts TunnelServerOptions) *TunnelServer {
	observeAgentsOnce.Do(func() {
		if _, err := opts.Registry.ObserveAgents(otel.Meter(logging.InstrumentationName)); err != nil {
			otel.Handle(err)
		}
	})

	return &TunnelServer{
		MTLS:           opts.MTLS,
		registry:       opts.Registry,
		resolver:       opts.Resolver,
		statusCallback: opts.StatusCallback,
		eventCallback:  opts.EventCallback,
	}
}

// GRPCServerOptions returns the receive limit and stream interceptors for the tunnel service.
// It is served through ServeHTTP, so transport keepalive options belong on net/http.
func (s *TunnelServer) GRPCServerOptions() []grpc.ServerOption {
	recovery := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if panicErr := concurrency.PanicToError(recover()); panicErr != nil {
				slog.ErrorContext(ss.Context(), "panic in gRPC tunnel stream", "method", info.FullMethod, "error", panicErr)
				err = status.Error(codes.Internal, "internal tunnel error")
			}
		}()
		return handler(srv, ss)
	}

	logStream := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		duration := time.Since(start)
		switch {
		case err == nil:
			slog.DebugContext(ss.Context(), "gRPC stream completed", "method", info.FullMethod, "duration", duration)
		case isExpectedReceiveError(err):
			slog.DebugContext(ss.Context(), "gRPC stream closed", "method", info.FullMethod, "duration", duration, "error", err)
		default:
			slog.WarnContext(ss.Context(), "gRPC stream failed", "method", info.FullMethod, "duration", duration, "error", err)
		}
		return err
	}

	auth := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if info.FullMethod != tunnelpb.TunnelService_Connect_FullMethodName {
			return handler(srv, ss)
		}

		ctx := ss.Context()
		md, _ := metadata.FromIncomingContext(ctx)
		token, _ := agentToken(md.Get)
		envID, err := s.resolveEnvironment(ctx, token)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid agent token")
		}

		p, _ := peer.FromContext(ctx)
		tlsInfo, isTLS := kit.FromPtr(p).AuthInfo.(credentials.TLSInfo)
		state := kit.Ternary(isTLS, &tlsInfo.State, nil)
		if identityErr := s.requireCertificateIdentity(state, envID); identityErr != nil {
			return status.Error(codes.Unauthenticated, identityErr.Error())
		}

		return handler(srv, &contextualServerStream{
			ServerStream: ss,
			ctx: context.WithValue(ctx, streamIdentityKey{}, streamIdentity{
				environmentID: envID,
				securityMode:  kit.Ternary(hasVerifiedPeerCertificate(state), "mtls", "token"),
			}),
		})
	}

	return []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxGRPCTunnelMessageSize),
		grpc.ChainStreamInterceptor(recovery, logStream, auth),
	}
}

func GRPCTunnelHandler(cfg GRPCTunnelHandlerConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, tunnelpb.TunnelService_Connect_FullMethodName) &&
			!strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "application/grpc") {
			cfg.Next.ServeHTTP(w, r)
			return
		}

		_, methodSuffix, hasMethod := strings.Cut(r.URL.Path, tunnelpb.TunnelService_Connect_FullMethodName)
		path := cmp.Or(
			kit.Ternary(strings.HasSuffix(r.URL.Path, tunnelConnectPath), tunnelpb.TunnelService_Connect_FullMethodName, ""),
			kit.Ternary(hasMethod, tunnelpb.TunnelService_Connect_FullMethodName+methodSuffix, ""),
			r.URL.Path,
		)
		_, port, splitErr := net.SplitHostPort(r.RemoteAddr)
		remoteAddr := kit.Ternary(splitErr == nil, net.JoinHostPort(cfg.ExtractIP(r), port), r.RemoteAddr)
		if path == r.URL.Path && remoteAddr == r.RemoteAddr {
			cfg.GRPC.ServeHTTP(w, r)
			return
		}

		routed := r.Clone(r.Context())
		routed.URL.Path = path
		routed.RequestURI = kit.Ternary(path == r.URL.Path, r.RequestURI, path)
		routed.RemoteAddr = remoteAddr
		cfg.GRPC.ServeHTTP(w, routed)
	})
}

func (s *TunnelServer) HandleConnect(c *echo.Context) error {
	req := c.Request()
	ctx := req.Context()

	conn, err := wshub.Accept(c.Response(), req, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to upgrade agent tunnel connection", "error", err)
		return nil
	}

	tunnelConn := NewWebSocketTunnelConn(conn)
	reject := func(reason string) error {
		if reason != "" {
			_ = tunnelConn.Send(&TunnelMessage{Type: MessageTypeRegisterResponse, Error: reason})
		}
		_ = tunnelConn.Close()
		return nil
	}

	registerMsg, err := tunnelConn.Receive()
	if err != nil {
		slog.WarnContext(ctx, "Failed to receive websocket agent tunnel registration", "error", err)
		return reject("")
	}
	if registerMsg == nil || registerMsg.Type != MessageTypeRegister {
		slog.WarnContext(ctx, "Websocket agent tunnel missing register message")
		return reject("")
	}

	// Proxies can drop upgrade headers or consume the client certificate, so the register message's token is the fallback claim.
	headerToken, _ := agentToken(req.Header.Values)
	token := cmp.Or(headerToken, strings.TrimSpace(registerMsg.AgentToken))
	if token == "" {
		slog.WarnContext(ctx, "Agent tunnel connection attempt without token")
		return reject("agent token required")
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token", "error", err)
		return reject("invalid agent token")
	}
	if identityErr := s.requireCertificateIdentity(req.TLS, envID); identityErr != nil {
		slog.WarnContext(ctx, "Rejected websocket agent tunnel with mismatched client certificate", "environmentId", envID, "error", identityErr)
		return reject("client certificate does not match environment")
	}

	s.manageConnectedTunnel(ctx, registerMsg, AgentTunnelConfig{
		Conn:          tunnelConn,
		EnvironmentID: envID,
		SecurityMode:  kit.Ternary(hasVerifiedPeerCertificate(req.TLS), "mtls", "token"),
		RemoteAddr:    c.RealIP(),
	})
	return nil
}

// Connect is the gRPC bidi stream handler for agent connections.
func (s *TunnelServer) Connect(stream grpc.BidiStreamingServer[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]) error {
	ctx := stream.Context()

	firstMsg, err := stream.Recv()
	switch {
	case errors.Is(err, io.EOF):
		return status.Error(codes.Unauthenticated, "register message required")
	case err != nil:
		return err
	}

	registerMsg, err := agentProtoToTunnelMessage(firstMsg)
	if err != nil || registerMsg.Type != MessageTypeRegister {
		return status.Error(codes.Unauthenticated, "first message must be register")
	}

	identity, _ := ctx.Value(streamIdentityKey{}).(streamIdentity)
	if strings.TrimSpace(identity.environmentID) == "" {
		return status.Error(codes.Unauthenticated, "authenticated environment is missing from stream context")
	}

	s.manageConnectedTunnel(ctx, registerMsg, AgentTunnelConfig{
		Conn:          NewGRPCManagerTunnelConn(stream),
		EnvironmentID: identity.environmentID,
		SecurityMode:  identity.securityMode,
		RemoteAddr:    grpcPeerHost(ctx),
	})
	return nil
}

func grpcPeerHost(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}

func (s *TunnelServer) resolveEnvironment(ctx context.Context, token string) (string, error) {
	if s.resolver == nil {
		return "", errors.New("agent resolver is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("agent token required")
	}
	return s.resolver(ctx, token)
}

// agentToken returns the first agent token from X-Arcane-Agent-Token, X-API-Key, then
// "Authorization: Bearer", for proxies that strip custom headers, plus the header that supplied it.
func agentToken(values func(key string) []string) (string, string) {
	for _, header := range []string{HeaderAgentToken, HeaderAPIKey} {
		if tokens := kit.TrimNonEmpty(values(header)); len(tokens) > 0 {
			return tokens[0], header
		}
	}
	for _, value := range values(HeaderAuthorization) {
		if token := ExtractBearerToken(value); token != "" {
			return token, HeaderAuthorization
		}
	}
	return "", ""
}

func (s *TunnelServer) manageConnectedTunnel(ctx context.Context, registerMsg *TunnelMessage, cfg AgentTunnelConfig) {
	tunnel := NewAgentTunnel(AgentTunnelConfig{
		Conn:          cfg.Conn,
		Host:          registerMsg.Host,
		EnvironmentID: cfg.EnvironmentID,
		AgentInstance: strings.TrimSpace(registerMsg.AgentInstance),
		SecurityMode:  cfg.SecurityMode,
		RemoteAddr:    cfg.RemoteAddr,
		Capabilities:  registerMsg.Capabilities,
	})

	// The span covers the connection lifetime; ctx stays unparented so per-message work doesn't nest under it.
	_, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "agent.tunnel "+cfg.Conn.Transport(),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("arcane.agent.transport", cfg.Conn.Transport()),
			attribute.String("arcane.environment.id", tunnel.EnvironmentID),
			attribute.String("arcane.agent.session_id", tunnel.SessionID),
			attribute.String("arcane.agent.security_mode", tunnel.SecurityMode),
			attribute.String("arcane.agent.instance", tunnel.AgentInstance),
		))
	logging.EndSpan(span, s.serveConnectedTunnel(ctx, tunnel))
}

func (s *TunnelServer) serveConnectedTunnel(ctx context.Context, tunnel *AgentTunnel) error {
	callbackCtx := context.WithoutCancel(ctx)
	envID := tunnel.EnvironmentID
	if !slices.Contains(tunnel.Capabilities, tunnelCapabilityMux) {
		slog.WarnContext(ctx, "Rejected agent without multiplexed data plane support", "environmentId", envID, "agentInstanceId", tunnel.AgentInstance)
		_ = tunnel.Conn.Send(&TunnelMessage{Type: MessageTypeRegisterResponse, EnvironmentID: envID, Error: errAgentMuxRequired.Error()})
		_ = tunnel.CloseWithReason(errAgentMuxRequired.Error())
		return errAgentMuxRequired
	}

	registration, err := s.registry.RegisterSession(callbackCtx, tunnel)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to register agent session", "environmentId", envID, "agentInstanceId", tunnel.AgentInstance, "error", err)
		_ = tunnel.CloseWithReason("agent session registration unavailable")
		return err
	}
	if !registration.Accepted {
		slog.WarnContext(ctx, "Rejected duplicate agent session", "environmentId", envID, "agentInstanceId", tunnel.AgentInstance, "reason", registration.Reason)
		_ = tunnel.Conn.Send(&TunnelMessage{Type: MessageTypeRegisterResponse, EnvironmentID: envID, Error: registration.Reason})
		_ = tunnel.CloseWithReason(registration.Reason)
		return errors.New(registration.Reason)
	}

	defer func() {
		if s.registry.UnregisterCurrent(callbackCtx, tunnel) {
			slog.InfoContext(ctx, "Agent disconnected", "environmentId", envID, "sessionId", tunnel.SessionID)
			s.updateConnectionStatus(callbackCtx, tunnel, false)
		}
	}()

	slog.InfoContext(ctx, "Agent connected", "environmentId", envID, "sessionId", tunnel.SessionID, "securityMode", tunnel.SecurityMode,
		"transport", tunnel.Conn.Transport())

	if sendErr := tunnel.Conn.Send(&TunnelMessage{
		Type:          MessageTypeRegisterResponse,
		Accepted:      true,
		EnvironmentID: envID,
		SessionID:     tunnel.SessionID,
		SecurityMode:  tunnel.SecurityMode,
		Capabilities:  []string{tunnelCapabilityMuxGrant},
		DrainPrevious: registration.DrainPrevious,
	}); sendErr != nil {
		slog.WarnContext(ctx, "Failed to send register response", "environmentId", envID, "error", sendErr)
		_ = tunnel.CloseWithReason("")
		return sendErr
	}
	return s.serveMuxTunnel(ctx, tunnel, tunnel.Conn.muxConn(ctx))
}

func (s *TunnelServer) StartCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(tunnelStaleSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed := s.registry.CleanupStale(ctx, TunnelStaleTimeout)
			for _, tunnel := range removed {
				s.updateConnectionStatus(context.WithoutCancel(ctx), tunnel, false)
			}
			if len(removed) > 0 {
				slog.InfoContext(ctx, "Cleaned up stale tunnels", "count", len(removed))
			}
		}
	}
}

func (s *TunnelServer) updateConnectionStatus(ctx context.Context, tunnel *AgentTunnel, connected bool) {
	if s.statusCallback == nil {
		return
	}

	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if active, ok := s.registry.Active(tunnel.EnvironmentID).Get(); !connected && ok && active != tunnel {
		return
	}
	s.statusCallback(ctx, tunnel.EnvironmentID, connected)
}

func (s *contextualServerStream) Context() context.Context {
	return s.ctx
}

// requireCertificateIdentity checks a direct-TLS client certificate against envID. A nil state
// means TLS ended before Arcane, such as at a proxy, so only the token applies.
func (s *TunnelServer) requireCertificateIdentity(state *tls.ConnectionState, envID string) error {
	if s.MTLS.Mode == MTLSModeDisabled || state == nil {
		return nil
	}
	if !hasVerifiedPeerCertificate(state) {
		if s.MTLS.Mode == MTLSModeRequired {
			return errors.New("verified agent mTLS client certificate is required")
		}
		return nil
	}

	safeEnvID := generatedAssetNameSanitizer.ReplaceAllString(strings.TrimSpace(envID), "_")
	if safeEnvID == "" {
		return errors.New("environment ID is required for agent mTLS certificate identity check")
	}
	appURL := cmp.Or(strings.TrimSpace(s.MTLS.AppURL), strings.TrimSpace(httpx.ManagerBaseURL(s.MTLS.ManagerURL)))
	expected := certgen.BuildAgentMTLSURISAN(appURL, safeEnvID)
	if expected == nil {
		return errors.New("agent mTLS trust domain is required for certificate identity check")
	}
	if !certificateHasURISAN(state.VerifiedChains[0][0], expected) {
		return fmt.Errorf("verified agent mTLS client certificate does not match environment %s", strings.TrimSpace(envID))
	}
	return nil
}
