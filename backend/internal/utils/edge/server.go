package edge

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/utils/remenv"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	// TunnelStaleTimeout is how long before a tunnel is considered stale
	TunnelStaleTimeout = 2 * time.Minute
)

var tunnelUpgrader = websocket.Upgrader{
	ReadBufferSize:    64 * 1024,
	WriteBufferSize:   64 * 1024,
	EnableCompression: true,
	CheckOrigin:       func(r *http.Request) bool { return true },
}

// EnvironmentResolver resolves an agent token to an environment ID
type EnvironmentResolver func(ctx context.Context, token string) (environmentID string, err error)

// StatusUpdateCallback is called when an edge agent connects or disconnects
// The connected parameter is true on connect, false on disconnect
type StatusUpdateCallback func(ctx context.Context, environmentID string, connected bool)

// TunnelServer handles incoming edge agent connections on the manager side
type TunnelServer struct {
	registry       *TunnelRegistry
	resolver       EnvironmentResolver
	statusCallback StatusUpdateCallback
	cleanupDone    chan struct{}
}

// NewTunnelServer creates a new tunnel server
func NewTunnelServer(resolver EnvironmentResolver, statusCallback StatusUpdateCallback) *TunnelServer {
	return &TunnelServer{
		registry:       GetRegistry(),
		resolver:       resolver,
		statusCallback: statusCallback,
		cleanupDone:    make(chan struct{}),
	}
}

// HandleConnect is the WebSocket handler for edge agent connections
// This is registered at /api/tunnel/connect
func (s *TunnelServer) HandleConnect(c *gin.Context) {
	ctx := c.Request.Context()
	callbackCtx := context.WithoutCancel(ctx)

	// Get agent token from headers
	token := c.GetHeader(remenv.HeaderAgentToken)
	if token == "" {
		token = c.GetHeader(remenv.HeaderAPIKey)
	}
	if token == "" {
		slog.WarnContext(ctx, "Edge tunnel connection attempt without token")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "agent token required"})
		return
	}

	// Resolve token to environment ID
	envID, err := s.resolver(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
		return
	}

	// Upgrade to WebSocket
	conn, err := tunnelUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to upgrade edge tunnel connection", "error", err)
		return
	}

	slog.InfoContext(ctx, "Edge agent connected", "environment_id", envID)

	// Create and register the tunnel
	tunnel := NewAgentTunnel(envID, conn)
	s.registry.Register(envID, tunnel)

	// Update environment status to online
	if s.statusCallback != nil {
		s.statusCallback(callbackCtx, envID, true)
	}

	// Ensure cleanup on disconnect
	defer func() {
		s.registry.Unregister(envID)
		slog.InfoContext(ctx, "Edge agent disconnected", "environment_id", envID)
		// Update environment status to offline
		if s.statusCallback != nil {
			s.statusCallback(callbackCtx, envID, false)
		}
	}()

	// Process messages from the agent
	s.messageLoop(ctx, tunnel)
}

// messageLoop processes incoming messages from the agent
func (s *TunnelServer) messageLoop(ctx context.Context, tunnel *AgentTunnel) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := tunnel.Conn.Receive()
			if err != nil {
				if !websocket.IsCloseError(err,
					websocket.CloseNormalClosure,
					websocket.CloseGoingAway,
					websocket.CloseNoStatusReceived) {
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
	case MessageTypeStreamData, MessageTypeStreamEnd, MessageTypeWebSocketData, MessageTypeWebSocketClose:
		s.deliverStream(ctx, tunnel, msg)
	case MessageTypeRequest, MessageTypeHeartbeatAck, MessageTypeWebSocketStart:
		slog.DebugContext(ctx, "Ignoring message type from agent", "type", msg.Type, "environment_id", tunnel.EnvironmentID)
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
		default:
			slog.DebugContext(ctx, "Stream data dropped due to backpressure", "id", msg.ID)
		}
	}
}

// StartCleanupLoop periodically cleans up stale tunnels
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

// WaitForCleanupDone blocks until the cleanup loop has stopped
func (s *TunnelServer) WaitForCleanupDone() {
	<-s.cleanupDone
}

// RegisterTunnelRoutes registers the tunnel WebSocket endpoint and returns the server
// for graceful shutdown. Call server.WaitForCleanupDone() after canceling the context.
func RegisterTunnelRoutes(ctx context.Context, group *gin.RouterGroup, resolver EnvironmentResolver, statusCallback StatusUpdateCallback) *TunnelServer {
	server := NewTunnelServer(resolver, statusCallback)
	go server.StartCleanupLoop(ctx)
	group.GET("/tunnel/connect", server.HandleConnect)
	slog.Info("Registered edge tunnel endpoint at /api/tunnel/connect")
	return server
}
