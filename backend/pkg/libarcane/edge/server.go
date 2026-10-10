package edge

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	certgen "github.com/getarcaneapp/arcane/cli/v2/pkg/generate"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/acfs/atomic"
	kit "go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

const (
	// TunnelStaleTimeout is how long before a tunnel is considered stale.
	TunnelStaleTimeout = 2 * time.Minute
	// tunnelStaleSweepInterval backs up read-deadline liveness, so it runs well under TunnelStaleTimeout.
	tunnelStaleSweepInterval = time.Minute
	// pendingDeliveryTimeout is how long one queued message may wait for a slow consumer.
	pendingDeliveryTimeout = 5 * time.Second
	// maxPendingBacklogBytes and maxPendingBacklogMessages bound how far a consumer may fall behind.
	maxPendingBacklogBytes    = 32 << 20
	maxPendingBacklogMessages = 4096
)

// observeAgentsOnce registers the connected-agents gauge for the first (manager) tunnel server.
var observeAgentsOnce sync.Once

// EnvironmentResolver resolves an agent token to an environment ID.
type EnvironmentResolver func(ctx context.Context, token string) (environmentID string, err error)

// EnvironmentNameResolver resolves a display name for an environment ID.
type EnvironmentNameResolver func(ctx context.Context, environmentID string) (environmentName string, err error)

// StatusUpdateCallback is called when an edge agent connects or disconnects.
// The connected parameter is true on connect, false on disconnect.
type StatusUpdateCallback func(ctx context.Context, environmentID string, connected bool)

// EventCallback is called when an edge agent publishes an event.
type EventCallback func(ctx context.Context, environmentID string, event *TunnelEvent) error

// EnrollmentCallback is called after a successful manager-side edge mTLS enrollment.
// reenrolled is true when the environment had already enrolled before the cooldown.
type EnrollmentCallback func(ctx context.Context, environmentID, remoteAddr string, certIssued, caGenerated, reenrolled bool)

// NewTunnelServerWithRegistry creates a new tunnel server using an injected tunnel registry.
func NewTunnelServerWithRegistry(registry *TunnelRegistry, resolver EnvironmentResolver, statusCallback StatusUpdateCallback) *TunnelServer {
	if registry == nil {
		registry = NewTunnelRegistry()
	}
	observeAgentsOnce.Do(func() {
		if _, err := registry.ObserveAgents(otel.Meter(tracing.InstrumentationName)); err != nil {
			otel.Handle(err)
		}
	})

	return &TunnelServer{
		registry:       registry,
		resolver:       resolver,
		statusCallback: statusCallback,
		cleanupDone:    make(chan struct{}),
	}
}

// GRPCServerOptions returns the receive limit and stream interceptors for the tunnel service.
// It is served through ServeHTTP, so transport keepalive options belong on net/http.
func (s *TunnelServer) GRPCServerOptions() []grpc.ServerOption {
	recovery := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if panicErr := utils.PanicToError(recover()); panicErr != nil {
				slog.ErrorContext(ss.Context(), "panic in gRPC tunnel stream", "method", info.FullMethod, "error", panicErr)
				err = status.Error(codes.Internal, "internal tunnel error")
			}
		}()
		return handler(srv, ss)
	}

	logging := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
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
		// TunnelService only exposes Connect; keep this gate aligned with tunnel.proto.
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

		var state *tls.ConnectionState
		if p, ok := peer.FromContext(ctx); ok {
			if tlsInfo, isTLS := p.AuthInfo.(credentials.TLSInfo); isTLS {
				state = &tlsInfo.State
			}
		}
		if identityErr := s.requireCertificateIdentity(state, envID); identityErr != nil {
			return status.Error(codes.Unauthenticated, identityErr.Error())
		}

		identity := streamIdentity{environmentID: envID, securityMode: kit.Ternary(hasVerifiedPeerCertificate(state), "mtls", "token")}
		return handler(srv, &contextualServerStream{ServerStream: ss, ctx: context.WithValue(ctx, streamIdentityKey{}, identity)})
	}

	return []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxGRPCTunnelMessageSize),
		grpc.ChainStreamInterceptor(recovery, logging, auth),
	}
}

// HandleConnect is the WebSocket handler for edge agent connections.
// This is registered at /api/tunnel/connect.
func (s *TunnelServer) HandleConnect(c *echo.Context) error {
	req := c.Request()
	ctx := req.Context()

	// Agents authenticate with tokens or mTLS rather than browser cookies, so no Origin check is needed.
	conn, err := wshub.Accept(c.Response(), req, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to upgrade edge tunnel connection", "error", err)
		return nil
	}

	tunnelConn := NewTunnelConn(conn)
	reject := func(reason string) error {
		if reason != "" {
			_ = tunnelConn.Send(&TunnelMessage{Type: MessageTypeRegisterResponse, Error: reason})
		}
		_ = tunnelConn.Close()
		return nil
	}

	registerMsg, err := tunnelConn.Receive()
	if err != nil {
		slog.WarnContext(ctx, "Failed to receive websocket edge tunnel registration", "error", err)
		return reject("")
	}
	if registerMsg == nil || registerMsg.Type != MessageTypeRegister {
		slog.WarnContext(ctx, "Websocket edge tunnel missing register message")
		return reject("")
	}

	// Headers can be lost on the upgrade path, and proxy-terminated mTLS consumes the client
	// certificate, so the register message's token is the fallback environment claim.
	token, _ := agentToken(req.Header.Values)
	token = cmp.Or(token, strings.TrimSpace(registerMsg.AgentToken))
	if token == "" {
		slog.WarnContext(ctx, "Edge tunnel connection attempt without token")
		return reject("agent token required")
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token", "error", err)
		return reject("invalid agent token")
	}
	if identityErr := s.requireCertificateIdentity(req.TLS, envID); identityErr != nil {
		slog.WarnContext(ctx, "Rejected websocket edge tunnel with mismatched client certificate", "environmentId", envID, "error", identityErr)
		return reject("client certificate does not match environment")
	}

	s.manageConnectedTunnel(ctx, envID, tunnelConn, registerMsg, kit.Ternary(hasVerifiedPeerCertificate(req.TLS), "mtls", "token"))
	return nil
}

// HandleMTLSEnroll returns manager-generated edge client certificates for the calling environment.
// The response contains private keys, so response-body logging must stay disabled here.
func (s *TunnelServer) HandleMTLSEnroll(c *echo.Context) error {
	req := c.Request()
	ctx := req.Context()
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")

	if !usesGeneratedManagerCA(s.Config) {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "edge mTLS enrollment is not available"})
	}

	token, _ := agentToken(req.Header.Values)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "agent token required"})
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve edge token for mTLS enrollment", "error", err)
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid agent token"})
	}

	envName := ""
	if s.NameResolver != nil {
		resolvedName, resolveErr := s.NameResolver(ctx, envID)
		if resolveErr != nil {
			slog.WarnContext(ctx, "Failed to resolve environment name for edge mTLS enrollment", "environmentId", envID, "error", resolveErr)
		} else {
			envName = resolvedName
		}
	}

	// The marker holds the last enrollment time; within the cooldown the existing assets are re-served.
	failState := func(stateErr error) error {
		slog.ErrorContext(ctx, "Failed to read edge mTLS enrollment state", "environmentId", envID, "error", stateErr)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to read edge mTLS enrollment state"})
	}
	assetsDir, err := edgeMTLSAssetsDir(s.Config, managerMTLSDirName)
	if err != nil {
		return failState(err)
	}
	safeEnvID := sanitizedEnvID(envID)
	if safeEnvID == "" {
		return failState(errors.New("environment ID is required"))
	}
	markerPath := filepath.Join(assetsDir, generatedClientMTLSSubdir, safeEnvID, generatedMTLSEnrolledName)
	marker, readErr := os.ReadFile(markerPath)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return failState(readErr)
	}
	now := time.Now()
	previouslyEnrolled := readErr == nil
	enrollmentLimited := false
	if trimmed := strings.TrimSpace(string(marker)); trimmed != "" {
		enrolledAt, parseErr := time.Parse(time.RFC3339Nano, trimmed)
		if parseErr != nil {
			return failState(fmt.Errorf("failed to parse edge mTLS enrollment marker %s: %w", markerPath, parseErr))
		}
		enrollmentLimited = now.Sub(enrolledAt) < managerMTLSReenrollCooldown
	}

	if enrollmentLimited {
		cachedAssets, cacheErr := GenerateManagerClientMTLSAssetsWithContext(ctx, s.Config, envID, envName)
		if cacheErr == nil && cachedAssets != nil {
			slog.InfoContext(ctx, "Served cached edge mTLS enrollment during cooldown", "environmentId", envID, "remoteAddr", c.RealIP())
			return c.JSON(http.StatusOK, enrollMTLSResponse{Files: cachedAssets.Files})
		}
		if cacheErr != nil {
			slog.WarnContext(ctx, "Failed to re-serve cached edge mTLS enrollment during cooldown", "environmentId", envID, "error", cacheErr)
		}
		slog.WarnContext(ctx, "Rejected repeated edge mTLS enrollment during cooldown", "environmentId", envID, "remoteAddr", c.RealIP())
		return c.JSON(http.StatusTooManyRequests, map[string]any{"error": "edge mTLS enrollment was recently completed; retry later"})
	}

	assets, err := GenerateManagerClientMTLSAssetsWithContext(ctx, s.Config, envID, envName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate edge mTLS enrollment assets", "environmentId", envID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to generate edge mTLS assets"})
	}
	if assets == nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "edge mTLS enrollment assets unavailable"})
	}
	assets.Reenrolled = previouslyEnrolled
	if recordErr := atomic.WriteFile(markerPath, []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0o600); recordErr != nil {
		slog.ErrorContext(ctx, "Failed to record edge mTLS enrollment state", "environmentId", envID, "error", recordErr)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to record edge mTLS enrollment state"})
	}
	if assets.Reenrolled {
		slog.WarnContext(ctx, "Edge mTLS certificate assets re-enrolled", "environmentId", envID, "remoteAddr", c.RealIP(), "certIssued", assets.CertIssued)
	} else {
		slog.InfoContext(ctx, "Edge mTLS certificate assets enrolled", "environmentId", envID, "remoteAddr", c.RealIP(), "certIssued", assets.CertIssued)
	}

	if s.EnrollmentCallback != nil {
		s.EnrollmentCallback(context.WithoutCancel(ctx), envID, c.RealIP(), assets.CertIssued, assets.CAGenerated, assets.Reenrolled)
	}

	return c.JSON(http.StatusOK, enrollMTLSResponse{Files: assets.Files})
}

// Connect is the gRPC bidi stream handler for edge agent connections.
func (s *TunnelServer) Connect(stream grpc.BidiStreamingServer[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]) error {
	ctx := stream.Context()

	firstMsg, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.Unauthenticated, "register message required")
		}
		return err
	}

	registerMsg, err := agentProtoToTunnelMessage(firstMsg)
	if err != nil || registerMsg.Type != MessageTypeRegister {
		return status.Error(codes.Unauthenticated, "first message must be register")
	}

	// The auth interceptor already verified the token and client certificate for this stream.
	identity, _ := ctx.Value(streamIdentityKey{}).(streamIdentity)
	if strings.TrimSpace(identity.environmentID) == "" {
		return status.Error(codes.Unauthenticated, "authenticated environment is missing from stream context")
	}

	managerConn := NewGRPCManagerTunnelConn(stream)
	managerConn.parity = slices.Contains(registerMsg.Capabilities, tunnelCapabilityProtoParity)
	s.manageConnectedTunnel(ctx, identity.environmentID, managerConn, registerMsg, identity.securityMode)
	return nil
}

func (s *TunnelServer) resolveEnvironment(ctx context.Context, token string) (string, error) {
	if s.resolver == nil {
		return "", errors.New("edge resolver is not configured")
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
		if token := remenv.ExtractBearerToken(value); token != "" {
			return token, HeaderAuthorization
		}
	}
	return "", ""
}

// manageConnectedTunnel registers an authenticated agent session and serves its messages until it disconnects.
func (s *TunnelServer) manageConnectedTunnel(ctx context.Context, envID string, conn TunnelConnection, registerMsg *TunnelMessage, securityMode string) {
	callbackCtx := context.WithoutCancel(ctx)
	tunnel := NewAgentTunnelWithConn(envID, conn)
	tunnel.SessionID = uuid.New().String()
	tunnel.SecurityMode = securityMode
	tunnel.AgentInstance = strings.TrimSpace(registerMsg.AgentInstance)
	tunnel.Capabilities = registerMsg.Capabilities
	tunnel.Transport = conn.Transport()

	// The span covers the connection lifetime; ctx stays unparented so per-message work doesn't nest under it.
	_, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "edge.tunnel "+tunnel.Transport,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("arcane.edge.transport", tunnel.Transport),
			attribute.String("arcane.environment.id", envID),
			attribute.String("arcane.edge.session_id", tunnel.SessionID),
			attribute.String("arcane.edge.security_mode", securityMode),
			attribute.String("arcane.edge.agent_instance", tunnel.AgentInstance),
		))
	var spanErr error
	defer func() { tracing.End(span, spanErr) }()

	accepted, drainPrevious, rejectReason, err := s.registry.RegisterSession(callbackCtx, tunnel, TunnelStaleTimeout)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to register edge agent session", "environmentId", envID, "agentInstanceId", tunnel.AgentInstance, "error", err)
		_ = tunnel.CloseWithReason("edge agent session registration unavailable")
		spanErr = err
		return
	}
	if !accepted {
		slog.WarnContext(ctx, "Rejected duplicate edge agent session", "environmentId", envID, "agentInstanceId", tunnel.AgentInstance, "reason", rejectReason)
		_ = conn.Send(&TunnelMessage{Type: MessageTypeRegisterResponse, EnvironmentID: envID, Error: rejectReason})
		_ = tunnel.CloseWithReason(rejectReason)
		spanErr = errors.New(rejectReason)
		return
	}

	defer func() {
		removed, active := s.registry.UnregisterCurrent(callbackCtx, envID, tunnel)
		if !removed {
			return
		}
		slog.InfoContext(ctx, "Edge agent disconnected", "environmentId", envID, "sessionId", tunnel.SessionID)
		if !active {
			s.updateConnectionStatus(callbackCtx, tunnel, false)
		}
	}()

	slog.InfoContext(ctx, "Edge agent connected", "environmentId", envID, "sessionId", tunnel.SessionID, "securityMode", tunnel.SecurityMode)

	// Echo the agent's capabilities plus the manager's own; older agents ignore extra strings.
	capabilities := kit.Unique(append(slices.Clone(tunnel.Capabilities), tunnelCapabilityProtoParity, tunnelCapabilityChunkedRequest, tunnelCapabilityCommandCreditGrant))
	if sendErr := conn.Send(&TunnelMessage{
		Type:          MessageTypeRegisterResponse,
		Accepted:      true,
		EnvironmentID: envID,
		SessionID:     tunnel.SessionID,
		SecurityMode:  tunnel.SecurityMode,
		Capabilities:  capabilities,
		DrainPrevious: drainPrevious,
	}); sendErr != nil {
		slog.WarnContext(ctx, "Failed to send register response", "environmentId", envID, "error", sendErr)
		_ = tunnel.CloseWithReason("")
		spanErr = sendErr
		return
	}
	s.updateConnectionStatus(callbackCtx, tunnel, true)

	deliver := func(msg *TunnelMessage, closeStream bool) {
		value, _ := tunnel.Pending.Load(msg.ID)
		if pending, ok := value.(*PendingRequest); ok {
			pending.enqueue(ctx, tunnel, msg, closeStream)
			return
		}
		slog.DebugContext(ctx, "Received message for unknown request", "id", msg.ID, "type", msg.Type)
	}

	for ctx.Err() == nil {
		msg, receiveErr := conn.Receive()
		if receiveErr != nil {
			if !conn.IsExpectedReceiveError(receiveErr) {
				slog.WarnContext(ctx, "Error receiving from edge tunnel", "environmentId", envID, "error", receiveErr)
				spanErr = receiveErr
			}
			return
		}

		switch msg.Type {
		case MessageTypeHeartbeat:
			tunnel.UpdateHeartbeat()
			if ackErr := conn.Send(&TunnelMessage{ID: msg.ID, Type: MessageTypeHeartbeatAck}); ackErr != nil {
				slog.WarnContext(ctx, "Failed to send heartbeat ack", "error", ackErr)
			}
		case MessageTypeResponse, MessageTypeCommandAck, MessageTypeCommandOutput, MessageTypeCommandComplete, MessageTypeFileChunk:
			deliver(msg, false)
		case MessageTypeStreamData, MessageTypeStreamEnd, MessageTypeWebSocketData, MessageTypeWebSocketClose, MessageTypeStreamClose:
			deliver(msg, true)
		case MessageTypeEvent:
			if msg.Event == nil {
				slog.WarnContext(ctx, "Received event message without payload", "environmentId", envID)
				continue
			}
			if s.EventCallback == nil {
				continue
			}
			event := cloneTunnelEvent(msg.Event)
			go func() {
				eventCtx, cancel := context.WithTimeout(callbackCtx, 15*time.Second)
				defer cancel()
				if eventErr := s.EventCallback(eventCtx, envID, event); eventErr != nil {
					slog.WarnContext(eventCtx, "Failed to process edge event", "environmentId", envID, "type", event.Type, "error", eventErr)
				}
			}()
		case MessageTypeRequest, MessageTypeHeartbeatAck, MessageTypeWebSocketStart, MessageTypeRegisterResponse,
			MessageTypeCommandRequest, MessageTypeStreamOpen, MessageTypeCancelRequest, MessageTypeRegister, MessageTypeCommandCredit:
			slog.DebugContext(ctx, "Ignoring message type from agent", "type", msg.Type, "environmentId", envID)
		default:
			slog.WarnContext(ctx, "Unknown message type from agent", "type", msg.Type, "environmentId", envID)
		}
	}
}

// enqueue hands msg to the consumer without blocking the tunnel; queued messages wait in order and outlive
// a tunnel close. A consumer that stalls past pendingDeliveryTimeout or the backlog limits is failed once.
func (p *PendingRequest) enqueue(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage, closeStream bool) {
	retained := func(m *TunnelMessage) int {
		size := len(m.Body) + len(m.Error)
		for k, v := range m.Headers {
			size += len(k) + len(v)
		}
		for k, v := range m.Metadata {
			size += len(k) + len(v)
		}
		return size
	}

	fail := func(stalled error) {
		// A consumer that already finished has nothing left to fail.
		if !tunnel.Pending.CompareAndDelete(msg.ID, p) {
			return
		}
		select {
		case p.failureCh <- stalled:
		default:
		}
		slog.WarnContext(ctx, "Failed slow pending request consumer", "id", msg.ID, "type", msg.Type, "error", stalled)
		if !closeStream {
			return
		}
		if closeErr := tunnel.Conn.Send(&TunnelMessage{ID: msg.ID, Type: MessageTypeStreamClose, Error: stalled.Error()}); closeErr != nil {
			slog.DebugContext(ctx, "Failed to close slow edge stream", "id", msg.ID, "error", closeErr)
		}
	}

	p.mu.Lock()
	if p.failed {
		p.mu.Unlock()
		return
	}
	if !p.draining {
		select {
		case p.ResponseCh <- msg:
			p.mu.Unlock()
			return
		default:
		}
		p.draining = true
		go func() {
			timer := time.NewTimer(pendingDeliveryTimeout)
			defer timer.Stop()
			for {
				p.mu.Lock()
				if p.failed || len(p.backlog) == 0 {
					p.draining = false
					p.mu.Unlock()
					return
				}
				next := p.backlog[0]
				p.backlog[0] = nil
				p.backlog = p.backlog[1:]
				p.backlogBytes -= retained(next)
				p.mu.Unlock()

				timer.Reset(pendingDeliveryTimeout)
				select {
				case p.ResponseCh <- next:
				case <-timer.C:
					p.mu.Lock()
					stalled := !p.failed
					p.failed, p.backlog = true, nil
					p.mu.Unlock()
					if stalled {
						fail(fmt.Errorf("consumer for pending request %s stalled for %s", next.ID, pendingDeliveryTimeout))
					}
					return
				}
			}
		}()
	}

	p.backlog = append(p.backlog, msg)
	p.backlogBytes += retained(msg)
	overflow := p.backlogBytes > maxPendingBacklogBytes || len(p.backlog) > maxPendingBacklogMessages
	if overflow {
		p.failed, p.backlog = true, nil
	}
	p.mu.Unlock()
	if overflow {
		fail(fmt.Errorf("consumer for pending request %s fell more than %d messages or %d bytes behind", msg.ID, maxPendingBacklogMessages, maxPendingBacklogBytes))
	}
}

// StartCleanupLoop periodically cleans up stale tunnels.
func (s *TunnelServer) StartCleanupLoop(ctx context.Context) {
	defer s.cleanupDoneOnce.Do(func() { close(s.cleanupDone) })
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

// updateConnectionStatus reports a connect or disconnect, skipping a disconnect
// when a newer live tunnel already serves the environment.
func (s *TunnelServer) updateConnectionStatus(ctx context.Context, tunnel *AgentTunnel, connected bool) {
	if s.statusCallback == nil {
		return
	}

	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if !connected {
		if active, _ := s.registry.Get(tunnel.EnvironmentID).Get(); active != tunnel && active.connected() {
			return
		}
	}
	s.statusCallback(ctx, tunnel.EnvironmentID, connected)
}

// WaitForCleanupDone blocks until the cleanup loop has stopped.
func (s *TunnelServer) WaitForCleanupDone() {
	<-s.cleanupDone
}

func (s *contextualServerStream) Context() context.Context {
	return s.ctx
}

// requireCertificateIdentity checks a direct-TLS client certificate against envID. A nil state
// means TLS ended before Arcane, such as at a proxy, so only the token applies.
func (s *TunnelServer) requireCertificateIdentity(state *tls.ConnectionState, envID string) error {
	mode := EdgeMTLSModeDisabled
	if s.Config != nil {
		mode = NormalizeEdgeMTLSMode(s.Config.EdgeMTLSMode)
	}
	if mode == EdgeMTLSModeDisabled || state == nil {
		return nil
	}
	if !hasVerifiedPeerCertificate(state) {
		if mode == EdgeMTLSModeRequired {
			return errors.New("verified edge mTLS client certificate is required")
		}
		return nil
	}

	safeEnvID := sanitizedEnvID(envID)
	if safeEnvID == "" {
		return errors.New("environment ID is required for edge mTLS certificate identity check")
	}
	appURL := cmp.Or(strings.TrimSpace(s.Config.AppURL), strings.TrimSpace(httpx.ManagerBaseURL(s.Config.ManagerApiUrl)))
	expected := certgen.BuildEdgeMTLSURISAN(appURL, safeEnvID)
	if expected == nil {
		return errors.New("edge mTLS trust domain is required for certificate identity check")
	}
	if !certificateHasURISAN(state.VerifiedChains[0][0], expected) {
		return fmt.Errorf("verified edge mTLS client certificate does not match environment %s", strings.TrimSpace(envID))
	}
	return nil
}

// IsInternalTunnelRequest reports whether a request is being dispatched by the
// in-process edge tunnel client instead of a real network listener.
func IsInternalTunnelRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	isInternal, _ := ctx.Value(internalTunnelRequestContextKey{}).(bool)
	return isInternal
}
