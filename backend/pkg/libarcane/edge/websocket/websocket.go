package websocket

import (
	"context"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/gin-gonic/gin"
)

type (
	// TunnelServer is an alias for the edge tunnel server.
	TunnelServer = edge.TunnelServer
	// AgentTunnel is an alias for active agent tunnels.
	AgentTunnel = edge.AgentTunnel
	// EnvironmentResolver resolves an agent token to an environment id.
	EnvironmentResolver = edge.EnvironmentResolver
	// StatusUpdateCallback handles connect/disconnect status updates.
	StatusUpdateCallback = edge.StatusUpdateCallback
)

// RegisterTunnelRoutes registers the legacy WebSocket tunnel endpoint.
func RegisterTunnelRoutes(ctx context.Context, group *gin.RouterGroup, resolver EnvironmentResolver, statusCallback StatusUpdateCallback) *TunnelServer {
	//nolint:staticcheck // Legacy websocket compatibility wrapper intentionally forwards to deprecated implementation.
	return edge.RegisterTunnelRoutes(ctx, group, resolver, statusCallback)
}

// StartTunnelClientWithErrors starts the legacy WebSocket tunnel client.
func StartTunnelClientWithErrors(ctx context.Context, cfg *config.Config, handler http.Handler) (<-chan error, error) {
	return edge.StartTunnelClientWithErrors(ctx, cfg, handler)
}

// ProxyWebSocketRequest proxies websocket streams via an active edge tunnel.
func ProxyWebSocketRequest(c *gin.Context, tunnel *AgentTunnel, targetPath string) {
	//nolint:staticcheck // Legacy websocket compatibility wrapper intentionally forwards to deprecated implementation.
	edge.ProxyWebSocketRequest(c, tunnel, targetPath)
}
