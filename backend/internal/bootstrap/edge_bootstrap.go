package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	edgews "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/websocket"
	"github.com/gin-gonic/gin"
)

// registerEdgeTunnelRoutes configures the manager-side edge tunnel server.
// It registers WebSocket routes for websocket transport and prepares gRPC service state for grpc transport.
// Returns the TunnelServer for graceful shutdown.
func registerEdgeTunnelRoutes(ctx context.Context, cfg *config.Config, apiGroup *gin.RouterGroup, appServices *Services) *edge.TunnelServer {
	// Resolver that validates API key and returns the environment ID
	resolver := func(ctx context.Context, token string) (string, error) {
		// Use the ApiKeyService which properly validates the key hash
		envID, err := appServices.ApiKey.GetEnvironmentByApiKey(ctx, token)
		if err != nil {
			return "", err
		}
		if envID == nil {
			return "", errors.New("API key is not linked to an environment")
		}
		return *envID, nil
	}

	// Status callback to update environment status when agent connects/disconnects
	statusCallback := func(ctx context.Context, envID string, connected bool) {
		var status string
		if connected {
			status = string(models.EnvironmentStatusOnline)
			// Update heartbeat when connecting
			if err := appServices.Environment.UpdateEnvironmentHeartbeat(ctx, envID); err != nil {
				slog.WarnContext(ctx, "Failed to update heartbeat on edge connect", "environment_id", envID, "error", err)
			}
		} else {
			status = string(models.EnvironmentStatusOffline)
		}

		updates := map[string]interface{}{
			"status": status,
		}
		_, err := appServices.Environment.UpdateEnvironment(ctx, envID, updates, nil, nil)
		if err != nil {
			slog.WarnContext(ctx, "Failed to update environment status on edge connect/disconnect", "environment_id", envID, "connected", connected, "error", err)
		} else {
			slog.InfoContext(ctx, "Updated environment status", "environment_id", envID, "status", status)
		}
	}

	if edge.UseGRPCEdgeTransport(cfg) {
		server := edge.NewTunnelServer(resolver, statusCallback)
		go server.StartCleanupLoop(ctx)
		slog.InfoContext(ctx, "Configured edge tunnel server for gRPC transport on shared HTTP listener")
		return server
	}

	return edgews.RegisterTunnelRoutes(ctx, apiGroup, resolver, statusCallback)
}
