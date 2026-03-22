package edge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// TunnelStaleTimeout is how long before a tunnel is considered stale.
	TunnelStaleTimeout = 2 * time.Minute
	// streamDeliveryTimeout bounds per-message delivery wait to a pending consumer.
	// This prevents silent data loss while avoiding indefinite blocking.
	streamDeliveryTimeout = 5 * time.Second
)

var tunnelUpgrader = websocket.Upgrader{
	ReadBufferSize:    64 * 1024,
	WriteBufferSize:   64 * 1024,
	EnableCompression: true,
	CheckOrigin:       func(r *http.Request) bool { return true },
}

// EnvironmentResolver resolves an agent token to an environment ID.
type EnvironmentResolver func(ctx context.Context, token string) (environmentID string, err error)

// StatusUpdateCallback is called when an edge agent connects or disconnects.
// The connected parameter is true on connect, false on disconnect.
type StatusUpdateCallback func(ctx context.Context, environmentID string, connected bool)

// EventCallback is called when an edge agent publishes an event.
type EventCallback func(ctx context.Context, environmentID string, event *TunnelEvent) error

// TunnelServer handles incoming edge agent connections on the manager side.
type TunnelServer struct {
	registry       *TunnelRegistry
	resolver       EnvironmentResolver
	statusCallback StatusUpdateCallback
	eventCallback  EventCallback
	cleanupDone    chan struct{}
	cfg            *Config
}

// NewTunnelServer creates a new tunnel server.
func NewTunnelServer(resolver EnvironmentResolver, statusCallback StatusUpdateCallback) *TunnelServer {
	return NewTunnelServerWithRegistry(GetRegistry(), resolver, statusCallback)
}

// NewTunnelServerWithRegistry creates a new tunnel server using an injected tunnel registry.
func NewTunnelServerWithRegistry(registry *TunnelRegistry, resolver EnvironmentResolver, statusCallback StatusUpdateCallback) *TunnelServer {
	if registry == nil {
		registry = NewTunnelRegistry()
	}

	return &TunnelServer{
		registry:       registry,
		resolver:       resolver,
		statusCallback: statusCallback,
		cleanupDone:    make(chan struct{}),
	}
}

type resolvedEnvironmentIDKey struct{}

// GRPCServerOptions returns the stream interceptor chain used by the tunnel service.
func (s *TunnelServer) GRPCServerOptions(ctx context.Context) []grpc.ServerOption {
	_ = ctx
	return []grpc.ServerOption{
		grpc.ChainStreamInterceptor(
			s.recoveryStreamInterceptorInternal(ctx),
			s.loggingStreamInterceptorInternal(ctx),
			s.authStreamInterceptorInternal(ctx),
		),
	}
}

// SetEventCallback configures the manager callback invoked for agent events.
func (s *TunnelServer) SetEventCallback(callback EventCallback) {
	s.eventCallback = callback
}

// SetConfig attaches edge tunnel runtime config to the manager-side server.
func (s *TunnelServer) SetConfig(cfg *Config) {
	s.cfg = cfg
}

// HandleConnect is the WebSocket handler for edge agent connections.
// This is registered at /api/tunnel/connect.
func (s *TunnelServer) HandleConnect(c *gin.Context) {
	ctx := c.Request.Context()
	callbackCtx := context.WithoutCancel(ctx)

	if err := s.requireClientCertificateInternal(c.Request); err != nil {
		slog.WarnContext(ctx, "Rejected websocket edge tunnel without required client certificate", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// Upgrade to WebSocket.
	conn, err := tunnelUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to upgrade edge tunnel connection", "error", err)
		return
	}

	tunnelConn := NewTunnelConn(conn)
	firstMsg, err := tunnelConn.Receive()
	if err != nil {
		slog.WarnContext(ctx, "Failed to receive websocket edge tunnel registration", "error", err)
		_ = tunnelConn.Close()
		return
	}
	if firstMsg == nil || firstMsg.Type != MessageTypeRegister {
		slog.WarnContext(ctx, "Websocket edge tunnel missing register message")
		_ = tunnelConn.Close()
		return
	}

	token := strings.TrimSpace(firstMsg.AgentToken)
	if token == "" {
		token = strings.TrimSpace(c.GetHeader(HeaderAgentToken))
	}
	if token == "" {
		token = strings.TrimSpace(c.GetHeader(HeaderAPIKey))
	}
	if token == "" {
		slog.WarnContext(ctx, "Edge tunnel connection attempt without token")
		_ = tunnelConn.Close()
		return
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token", "error", err)
		_ = tunnelConn.Send(&TunnelMessage{Type: MessageTypeRegisterResponse, Accepted: false, Error: "invalid agent token"})
		_ = tunnelConn.Close()
		return
	}

	tunnel := NewAgentTunnelWithConn(envID, tunnelConn)
	s.populateSessionMetadata(tunnel, firstMsg, requestSecurityModeInternal(c.Request))
	s.manageConnectedTunnel(ctx, callbackCtx, tunnel)
}

// HandleMTLSEnroll returns manager-generated edge client certificates for the
// calling environment when auto-generated edge mTLS is enabled.
func (s *TunnelServer) HandleMTLSEnroll(c *gin.Context) {
	ctx := c.Request.Context()

	if s == nil || s.cfg == nil || !shouldAutoGenerateManagerCAInternal(s.cfg) {
		c.JSON(http.StatusNotFound, gin.H{"error": "edge mTLS enrollment is not available"})
		return
	}

	token := strings.TrimSpace(c.GetHeader(HeaderAgentToken))
	if token == "" {
		token = strings.TrimSpace(c.GetHeader(HeaderAPIKey))
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "agent token required"})
		return
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve edge token for mTLS enrollment", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
		return
	}

	assets, err := GenerateManagerClientMTLSAssets(s.cfg, envID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate edge mTLS enrollment assets", "environment_id", envID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate edge mTLS assets"})
		return
	}
	if assets == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "edge mTLS enrollment assets unavailable"})
		return
	}

	c.JSON(http.StatusOK, enrollMTLSResponse{Files: assets.Files})
}

// Connect is the gRPC bidi stream handler for edge agent connections.
func (s *TunnelServer) Connect(stream grpc.BidiStreamingServer[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]) error {
	ctx := stream.Context()
	callbackCtx := context.WithoutCancel(ctx)

	firstMsg, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.Unauthenticated, "register message required")
		}
		return err
	}

	register := firstMsg.GetRegister()
	if register == nil {
		return status.Error(codes.Unauthenticated, "first message must be register")
	}

	envID, ok := resolvedEnvironmentIDFromContextInternal(ctx)
	if !ok || envID == "" {
		token := strings.TrimSpace(register.GetAgentToken())
		if token == "" {
			token = tokenFromMetadata(ctx)
		}

		var resolveErr error
		envID, resolveErr = s.resolveEnvironment(ctx, token)
		if resolveErr != nil {
			slog.WarnContext(ctx, "Failed to resolve gRPC agent token", "error", resolveErr)
			_ = stream.Send(&tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_RegisterResponse{RegisterResponse: &tunnelpb.RegisterResponse{
				Accepted: false,
				Error:    "invalid agent token",
			}}})
			return status.Error(codes.Unauthenticated, "invalid agent token")
		}
	}

	tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(stream))
	s.populateSessionMetadata(tunnel, &TunnelMessage{
		AgentInstance: register.GetAgentInstanceId(),
		Capabilities:  append([]string(nil), register.GetCapabilities()...),
		ResumeSession: register.GetResumeSessionId(),
	}, securityModeFromGRPCContextInternal(ctx))
	s.manageConnectedTunnel(ctx, callbackCtx, tunnel)
	return nil
}

func (s *TunnelServer) populateSessionMetadata(tunnel *AgentTunnel, registerMsg *TunnelMessage, securityMode string) {
	if tunnel == nil {
		return
	}

	tunnel.SessionID = uuid.NewString()
	tunnel.SecurityMode = securityMode
	if tunnel.SecurityMode == "" {
		tunnel.SecurityMode = "token"
	}
	if registerMsg != nil {
		tunnel.AgentInstance = strings.TrimSpace(registerMsg.AgentInstance)
		tunnel.Capabilities = append([]string(nil), registerMsg.Capabilities...)
	}

	switch tunnel.Conn.(type) {
	case *GRPCManagerTunnelConn:
		tunnel.Transport = EdgeTransportGRPC
	default:
		tunnel.Transport = EdgeTransportWebSocket
	}
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

func tokenFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, key := range []string{
		strings.ToLower(HeaderAgentToken),
		strings.ToLower(HeaderAPIKey),
	} {
		values := md.Get(key)
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func (s *TunnelServer) manageConnectedTunnel(ctx context.Context, callbackCtx context.Context, tunnel *AgentTunnel) {
	accepted, drainPrevious, rejectReason := s.registry.RegisterSession(tunnel, TunnelStaleTimeout)
	if !accepted {
		slog.WarnContext(ctx, "Rejected duplicate edge agent session",
			"environment_id", tunnel.EnvironmentID,
			"agent_instance_id", tunnel.AgentInstance,
			"reason", rejectReason,
		)
		_ = tunnel.Conn.Send(&TunnelMessage{
			Type:          MessageTypeRegisterResponse,
			Accepted:      false,
			EnvironmentID: tunnel.EnvironmentID,
			Error:         rejectReason,
		})
		_ = tunnel.CloseWithReason(rejectReason)
		return
	}

	slog.InfoContext(ctx, "Edge agent connected",
		"environment_id", tunnel.EnvironmentID,
		"session_id", tunnel.SessionID,
		"security_mode", tunnel.SecurityMode,
	)

	if err := tunnel.Conn.Send(&TunnelMessage{
		Type:          MessageTypeRegisterResponse,
		Accepted:      true,
		EnvironmentID: tunnel.EnvironmentID,
		SessionID:     tunnel.SessionID,
		SecurityMode:  tunnel.SecurityMode,
		Capabilities:  append([]string(nil), tunnel.Capabilities...),
		DrainPrevious: drainPrevious,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to send register response", "environment_id", tunnel.EnvironmentID, "error", err)
		_ = tunnel.Close()
		_, _ = s.registry.UnregisterCurrent(tunnel.EnvironmentID, tunnel)
		return
	}

	if s.statusCallback != nil {
		s.statusCallback(callbackCtx, tunnel.EnvironmentID, true)
	}

	defer func() {
		removed, active := s.registry.UnregisterCurrent(tunnel.EnvironmentID, tunnel)
		if !removed {
			return
		}
		slog.InfoContext(ctx, "Edge agent disconnected", "environment_id", tunnel.EnvironmentID, "session_id", tunnel.SessionID)
		if s.statusCallback != nil && !active {
			s.statusCallback(callbackCtx, tunnel.EnvironmentID, false)
		}
	}()

	s.messageLoop(ctx, tunnel)
}

// messageLoop processes incoming messages from the agent.
func (s *TunnelServer) messageLoop(ctx context.Context, tunnel *AgentTunnel) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := tunnel.Conn.Receive()
			if err != nil {
				if !tunnel.Conn.IsExpectedReceiveError(err) {
					slog.WarnContext(ctx, "Error receiving from edge tunnel", "environment_id", tunnel.EnvironmentID, "error", err)
				}
				return
			}

			s.handleTunnelMessage(ctx, tunnel, msg)
		}
	}
}

func (s *TunnelServer) handleTunnelMessage(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage) {
	switch msg.Type {
	case MessageTypeHeartbeat:
		s.handleHeartbeat(ctx, tunnel, msg)
	case MessageTypeResponse:
		s.deliverResponse(ctx, tunnel, msg)
	case MessageTypeCommandAck, MessageTypeCommandOutput, MessageTypeCommandComplete, MessageTypeFileChunk:
		s.deliverResponse(ctx, tunnel, msg)
	case MessageTypeEvent:
		s.handleEvent(ctx, tunnel, msg)
	case MessageTypeStreamData, MessageTypeStreamEnd, MessageTypeWebSocketData, MessageTypeWebSocketClose, MessageTypeStreamClose:
		s.deliverStream(ctx, tunnel, msg)
	case MessageTypeRequest, MessageTypeHeartbeatAck, MessageTypeWebSocketStart, MessageTypeRegisterResponse, MessageTypeCommandRequest, MessageTypeStreamOpen, MessageTypeCancelRequest:
		slog.DebugContext(ctx, "Ignoring message type from agent", "type", msg.Type, "environment_id", tunnel.EnvironmentID)
	case MessageTypeRegister:
		slog.DebugContext(ctx, "Ignoring duplicate register message from agent", "environment_id", tunnel.EnvironmentID)
	default:
		slog.WarnContext(ctx, "Unknown message type from agent", "type", msg.Type, "environment_id", tunnel.EnvironmentID)
	}
}

func (s *TunnelServer) handleHeartbeat(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage) {
	tunnel.UpdateHeartbeat()
	ack := &TunnelMessage{
		ID:   msg.ID,
		Type: MessageTypeHeartbeatAck,
	}
	if err := tunnel.Conn.Send(ack); err != nil {
		slog.WarnContext(ctx, "Failed to send heartbeat ack", "error", err)
	}
}

func (s *TunnelServer) deliverResponse(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage) {
	if req, ok := tunnel.Pending.Load(msg.ID); ok {
		pending := req.(*PendingRequest)
		select {
		case pending.ResponseCh <- msg:
		default:
			slog.WarnContext(ctx, "Response channel full, dropping response", "id", msg.ID)
		}
		return
	}
	slog.WarnContext(ctx, "Received response for unknown request", "id", msg.ID)
}

func (s *TunnelServer) deliverStream(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage) {
	if req, ok := tunnel.Pending.Load(msg.ID); ok {
		pending := req.(*PendingRequest)
		select {
		case pending.ResponseCh <- msg:
		case <-ctx.Done():
			return
		case <-time.After(streamDeliveryTimeout):
			slog.WarnContext(ctx, "Timed out delivering stream message to pending consumer",
				"id", msg.ID,
				"type", msg.Type,
				"timeout", streamDeliveryTimeout,
			)
		}
	}
}

func (s *TunnelServer) handleEvent(ctx context.Context, tunnel *AgentTunnel, msg *TunnelMessage) {
	if msg.Event == nil {
		slog.WarnContext(ctx, "Received event message without payload", "environment_id", tunnel.EnvironmentID)
		return
	}
	if s.eventCallback == nil {
		return
	}

	eventCopy := cloneTunnelEvent(msg.Event)
	go func() {
		eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := s.eventCallback(eventCtx, tunnel.EnvironmentID, eventCopy); err != nil {
			slog.WarnContext(eventCtx, "Failed to process edge event", "environment_id", tunnel.EnvironmentID, "type", eventCopy.Type, "error", err)
		}
	}()
}

// StartCleanupLoop periodically cleans up stale tunnels.
func (s *TunnelServer) StartCleanupLoop(ctx context.Context) {
	defer close(s.cleanupDone)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count := s.registry.CleanupStale(TunnelStaleTimeout)
			if count > 0 {
				slog.InfoContext(ctx, "Cleaned up stale tunnels", "count", count)
			}
		}
	}
}

// WaitForCleanupDone blocks until the cleanup loop has stopped.
func (s *TunnelServer) WaitForCleanupDone() {
	<-s.cleanupDone
}

func (s *TunnelServer) authStreamInterceptorInternal(ctx context.Context) grpc.StreamServerInterceptor {
	_ = ctx
	//nolint:contextcheck // Stream interceptors receive request-scoped context from grpc.ServerStream.
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if info == nil || info.FullMethod != tunnelpb.TunnelService_Connect_FullMethodName {
			return handler(srv, ss)
		}

		if err := s.requireClientCertificateFromContextInternal(ss.Context()); err != nil {
			return status.Error(codes.Unauthenticated, err.Error())
		}

		token := tokenFromMetadata(ss.Context())
		envID, err := s.resolveEnvironment(ss.Context(), token)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid agent token")
		}

		ctx := context.WithValue(ss.Context(), resolvedEnvironmentIDKey{}, envID)
		return handler(srv, &contextualServerStream{ServerStream: ss, ctx: ctx})
	}
}

func (s *TunnelServer) loggingStreamInterceptorInternal(ctx context.Context) grpc.StreamServerInterceptor {
	_ = ctx
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		duration := time.Since(start)

		if err != nil {
			if isExpectedGRPCReceiveErrorInternal(err) {
				slog.DebugContext(ss.Context(), "gRPC stream closed", "method", info.FullMethod, "duration", duration, "error", err)
			} else {
				slog.WarnContext(ss.Context(), "gRPC stream failed", "method", info.FullMethod, "duration", duration, "error", err)
			}
			return err
		}

		slog.DebugContext(ss.Context(), "gRPC stream completed", "method", info.FullMethod, "duration", duration)
		return nil
	}
}

func (s *TunnelServer) recoveryStreamInterceptorInternal(ctx context.Context) grpc.StreamServerInterceptor {
	_ = ctx
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(ss.Context(), "panic in gRPC tunnel stream",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal tunnel error")
			}
		}()

		return handler(srv, ss)
	}
}

type contextualServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextualServerStream) Context() context.Context {
	return s.ctx
}

func resolvedEnvironmentIDFromContextInternal(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	envID, ok := ctx.Value(resolvedEnvironmentIDKey{}).(string)
	if !ok || strings.TrimSpace(envID) == "" {
		return "", false
	}

	return envID, true
}

func (s *TunnelServer) requireClientCertificateInternal(req *http.Request) error {
	if NormalizeEdgeMTLSMode(s.edgeMTLSModeInternal()) != EdgeMTLSModeRequired {
		return nil
	}
	if hasVerifiedPeerCertificateInternal(req.TLS) {
		return nil
	}
	return errors.New("verified client certificate required")
}

func (s *TunnelServer) requireClientCertificateFromContextInternal(ctx context.Context) error {
	if NormalizeEdgeMTLSMode(s.edgeMTLSModeInternal()) != EdgeMTLSModeRequired {
		return nil
	}

	p, ok := peer.FromContext(ctx)
	if !ok {
		return errors.New("verified client certificate required")
	}
	if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok && hasVerifiedPeerCertificateInternal(&tlsInfo.State) {
		return nil
	}
	return errors.New("verified client certificate required")
}

func (s *TunnelServer) edgeMTLSModeInternal() string {
	if s == nil || s.cfg == nil {
		return EdgeMTLSModeDisabled
	}
	return s.cfg.EdgeMTLSMode
}

func securityModeFromGRPCContextInternal(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "token"
	}
	return grpcContextSecurityModeInternal(*p)
}
