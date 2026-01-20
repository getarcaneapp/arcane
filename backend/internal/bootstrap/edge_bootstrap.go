package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/edge"
	"github.com/gin-gonic/gin"
)

// registerEdgeTunnelRoutes registers the edge tunnel WebSocket endpoint on the manager.
// This allows edge agents to connect and establish a tunnel for proxied requests.
// Returns the TunnelServer for graceful shutdown.
func registerEdgeTunnelRoutes(ctx context.Context, apiGroup *gin.RouterGroup, appServices *Services) *edge.TunnelServer {
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

	return edge.RegisterTunnelRoutes(ctx, apiGroup, resolver, statusCallback)
}
