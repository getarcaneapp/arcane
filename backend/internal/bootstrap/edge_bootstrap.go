package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/labstack/echo/v4"
)

// registerEdgeTunnelRoutes configures the manager-side edge tunnel server.
// It registers the WebSocket route and prepares gRPC service state on the shared listener.
// Returns the TunnelServer for graceful shutdown.
func registerEdgeTunnelRoutes(
	ctx context.Context,
	cfg *config.Config,
	apiGroup *echo.Group,
	appServices *Services,
) *edge.TunnelServer {
	// Resolver that validates API key and returns the environment ID
	resolver := func(ctx context.Context, token string) (string, error) {
		return appServices.Environment.ResolveEdgeEnvironmentByToken(ctx, token)
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
	server.SetConfig(&edge.Config{
		EdgeMTLSMode:       cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:     cfg.EdgeMTLSCAFile,
		EdgeMTLSCertFile:   cfg.EdgeMTLSCertFile,
		EdgeMTLSKeyFile:    cfg.EdgeMTLSKeyFile,
		EdgeMTLSServerName: cfg.EdgeMTLSServerName,
		EdgeMTLSAssetsDir:  cfg.EdgeMTLSAssetsDir,
		AppURL:             cfg.GetAppURL(),
		ManagerApiUrl:      cfg.ManagerApiUrl,
	})
	server.SetEnvironmentNameResolver(func(ctx context.Context, envID string) (string, error) {
		env, err := appServices.Environment.GetEnvironmentByID(ctx, envID)
		if err != nil {
			return "", err
		}
		if env == nil {
			return "", nil
		}
		return env.Name, nil
	})
	server.SetEventCallback(eventCallback)
	server.SetEnrollmentCallback(func(ctx context.Context, envID, remoteAddr string, certIssued bool, caGenerated bool, reenrolled bool) {
		if appServices.Event == nil {
			return
		}
		envName := ""
		if env, err := appServices.Environment.GetEnvironmentByID(ctx, envID); err == nil && env != nil {
			envName = env.Name
		}
		resourceType := "environment"
		envIDCopy := envID
		envNameCopy := envName
		_, _ = appServices.Event.CreateEvent(ctx, services.CreateEventRequest{
			Type:          models.EventTypeEnvironmentMTLSEnroll,
			Severity:      edgeMTLSEnrollmentSeverityInternal(reenrolled),
			Title:         "Edge mTLS enrollment",
			Description:   fmt.Sprintf("Edge agent completed mTLS enrollment from %s", remoteAddr),
			ResourceType:  &resourceType,
			ResourceID:    &envIDCopy,
			ResourceName:  &envNameCopy,
			EnvironmentID: &envIDCopy,
			Metadata:      models.JSON{"remoteAddr": remoteAddr, "reenrollment": reenrolled},
		})
		createEdgeMTLSIssueEventsInternal(ctx, appServices.Event, envIDCopy, envNameCopy, remoteAddr, certIssued, caGenerated, reenrolled)
	})
	go server.StartCleanupLoop(ctx)
	apiGroup.POST("/tunnel/poll", server.HandlePoll)
	// Rate-limit agent mTLS enrollment per-IP. Enrollment is authenticated
	// only by the agent token, so we cap bursts to mitigate brute-force or
	// token-abuse attempts without impacting normal agent lifecycles.
	apiGroup.POST("/tunnel/mtls/enroll", server.HandleMTLSEnroll, middleware.PerIPRateLimit(10, 3), middleware.PerAgentTokenRateLimit(10, 3))
	apiGroup.GET("/tunnel/connect", server.HandleConnect, middleware.PerIPRateLimit(60, 30), middleware.PerAgentTokenRateLimit(10, 3))
	slog.InfoContext(ctx, "Configured edge tunnel server",
		"poll_enabled", true,
		"grpc_enabled", !cfg.AgentMode,
		"websocket_enabled", true,
	)
	return server
}

func createEdgeMTLSIssueEventsInternal(ctx context.Context, eventService *services.EventService, envID string, envName string, remoteAddr string, certIssued bool, caGenerated bool, reenrolled bool) {
	if eventService == nil {
		return
	}
	if caGenerated {
		_, _ = eventService.CreateEvent(ctx, services.CreateEventRequest{
			Type:        models.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    models.EventSeverityInfo,
			Title:       "Edge mTLS CA generated",
			Description: "Arcane generated a new edge mTLS certificate authority",
			Metadata:    models.JSON{"remoteAddr": remoteAddr, "kind": "ca"},
		})
	}
	if certIssued {
		resourceType := "environment"
		_, _ = eventService.CreateEvent(ctx, services.CreateEventRequest{
			Type:          models.EventTypeEnvironmentMTLSCertIssued,
			Severity:      edgeMTLSCertIssuedSeverityInternal(reenrolled),
			Title:         "Edge mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an edge mTLS client certificate for environment '%s'", envName),
			ResourceType:  &resourceType,
			ResourceID:    &envID,
			ResourceName:  &envName,
			EnvironmentID: &envID,
			Metadata:      models.JSON{"remoteAddr": remoteAddr, "kind": "client", "reenrollment": reenrolled},
		})
	}
}

func edgeMTLSEnrollmentSeverityInternal(reenrolled bool) models.EventSeverity {
	if reenrolled {
		return models.EventSeverityWarning
	}
	return models.EventSeverityInfo
}

func edgeMTLSCertIssuedSeverityInternal(reenrolled bool) models.EventSeverity {
	if reenrolled {
		return models.EventSeverityWarning
	}
	return models.EventSeverityInfo
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

	_, err := eventService.CreateEvent(ctx, services.CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("environment"),
		ResourceID:    &envID,
		ResourceName:  &envName,
		EnvironmentID: &envID,
	})
	if err != nil {
		return fmt.Errorf("failed to create edge lifecycle event: %w", err)
	}

	return nil
}
