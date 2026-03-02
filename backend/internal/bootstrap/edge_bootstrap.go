package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/gin-gonic/gin"
)

// registerEdgeTunnelRoutes configures the manager-side edge tunnel server.
// It registers the WebSocket route and prepares gRPC service state on the shared listener.
// Returns the TunnelServer for graceful shutdown.
func registerEdgeTunnelRoutes(
	ctx context.Context,
	cfg *config.Config,
	apiGroup *gin.RouterGroup,
	appServices *Services,
) *edge.TunnelServer {
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
		envName := envID
		env, getErr := appServices.Environment.GetEnvironmentByID(ctx, envID)
		if getErr != nil {
			slog.WarnContext(ctx, "Failed to load environment before edge status update", "environment_id", envID, "error", getErr)
		} else if env != nil && env.Name != "" {
			envName = env.Name
		}

		if err := appServices.Environment.UpdateEnvironmentConnectionState(ctx, envID, connected); err != nil {
			slog.WarnContext(ctx, "Failed to update environment status on edge connect/disconnect", "environment_id", envID, "connected", connected, "error", err)
		} else {
			slog.InfoContext(ctx, "Updated edge environment connection state", "environment_id", envID, "connected", connected)
		}

		if err := createEdgeConnectionEvent(ctx, appServices.Event, envID, envName, connected); err != nil {
			slog.WarnContext(ctx, "Failed to create edge connection event", "environment_id", envID, "connected", connected, "error", err)
		}
	}

	eventCallback := func(ctx context.Context, envID string, evt *edge.TunnelEvent) error {
		if evt == nil {
			return fmt.Errorf("event payload is required")
		}

		var metadata models.JSON
		if len(evt.MetadataJSON) > 0 {
			metadata = models.JSON{}
			if err := json.Unmarshal(evt.MetadataJSON, &metadata); err != nil {
				return fmt.Errorf("failed to decode event metadata: %w", err)
			}
		}

		req := services.CreateEventRequest{
			Type:          models.EventType(evt.Type),
			Severity:      models.EventSeverity(evt.Severity),
			Title:         evt.Title,
			Description:   evt.Description,
			ResourceType:  optionalStringPtr(evt.ResourceType),
			ResourceID:    optionalStringPtr(evt.ResourceID),
			ResourceName:  optionalStringPtr(evt.ResourceName),
			UserID:        optionalStringPtr(evt.UserID),
			Username:      optionalStringPtr(evt.Username),
			EnvironmentID: &envID,
			Metadata:      metadata,
		}
		_, err := appServices.Event.CreateEvent(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to persist synced event: %w", err)
		}
		return nil
	}

	server := edge.NewTunnelServer(resolver, statusCallback)
	server.SetEventCallback(eventCallback)
	go server.StartCleanupLoop(ctx)
	apiGroup.GET("/tunnel/connect", server.HandleConnect)
	slog.InfoContext(ctx, "Configured edge tunnel server",
		"grpc_enabled", !cfg.AgentMode,
		"websocket_enabled", true,
	)
	return server
}

func optionalStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func createEdgeConnectionEvent(ctx context.Context, eventService *services.EventService, envID, envName string, connected bool) error {
	if eventService == nil {
		return nil
	}

	eventType := models.EventTypeEnvironmentDisconnect
	title := "Edge Agent Disconnected"
	description := fmt.Sprintf("Edge agent for environment '%s' disconnected", envName)
	severity := models.EventSeverityWarning

	if connected {
		eventType = models.EventTypeEnvironmentConnect
		title = "Edge Agent Connected"
		description = fmt.Sprintf("Edge agent for environment '%s' connected", envName)
		severity = models.EventSeveritySuccess
	}

	resourceType := "environment"
	_, err := eventService.CreateEvent(ctx, services.CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &envID,
		ResourceName:  &envName,
		EnvironmentID: &envID,
	})
	if err != nil {
		return fmt.Errorf("failed to create edge lifecycle event: %w", err)
	}

	return nil
}
