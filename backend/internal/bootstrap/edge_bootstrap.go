package bootstrap

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"
)

// registerEdgeTunnelRoutes configures the manager-side edge tunnel server.
// It registers the WebSocket route and prepares gRPC service state on the shared listener.
// Returns the TunnelServer for graceful shutdown.
func registerEdgeTunnelRoutes(
	ctx context.Context,
	lifecycle fx.Lifecycle,
	actorRuntime *actors.Runtime,
	cfg *config.Config,
	apiGroup *echo.Group,
	environmentService *environment.EnvironmentService,
	eventService *event.EventService,
	registry *edge.TunnelRegistry,
) *edge.TunnelServer {
	// Resolver that validates API key and returns the environment ID
	resolver := func(ctx context.Context, token string) (string, error) {
		return environmentService.ResolveEdgeEnvironmentByToken(ctx, token)
	}

	// Status callback to update environment status when agent connects/disconnects
	statusCallback := func(ctx context.Context, envID string, connected bool) {
		handleEdgeStatusChange(ctx, environmentService, eventService, envID, connected)
	}

	eventCallback := func(ctx context.Context, envID string, evt *edge.TunnelEvent) error {
		if evt == nil {
			return errors.New("event payload is required")
		}

		var metadata models.JSON
		if len(evt.MetadataJSON) > 0 {
			metadata = models.JSON{}
			if err := json.Unmarshal(evt.MetadataJSON, &metadata); err != nil {
				return errors.WrapIf(err, "failed to decode event metadata")
			}
		}

		req := event.CreateEventRequest{
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
		_, err := eventService.CreateEvent(ctx, req)
		if err != nil {
			return errors.WrapIf(err, "failed to persist synced event")
		}
		return nil
	}

	server := edge.NewTunnelServerWithRegistry(registry, resolver, statusCallback)
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
		env, err := environmentService.GetEnvironmentByID(ctx, envID)
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
		if eventService == nil {
			return
		}
		envName := ""
		if env, err := environmentService.GetEnvironmentByID(ctx, envID); err == nil && env != nil {
			envName = env.Name
		}
		envIDCopy := envID
		envNameCopy := envName
		_, _ = eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          models.EventTypeEnvironmentMTLSEnroll,
			Severity:      edgeMTLSEnrollmentSeverityInternal(reenrolled),
			Title:         "Edge mTLS enrollment",
			Description:   "Edge agent completed mTLS enrollment from " + remoteAddr,
			ResourceType:  new("environment"),
			ResourceID:    &envIDCopy,
			ResourceName:  &envNameCopy,
			EnvironmentID: &envIDCopy,
			Metadata:      models.JSON{"remoteAddr": remoteAddr, "reenrollment": reenrolled},
		})
		createEdgeMTLSIssueEventsInternal(ctx, eventService, envIDCopy, envNameCopy, remoteAddr, certIssued, caGenerated, reenrolled)
	})
	var cleanupRunner *actors.Runner
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var err error
			cleanupRunner, err = actors.NewRunner(ctx, actorRuntime, "edge-tunnel", "cleanup", "edge tunnel cleanup", 3, func(runCtx context.Context) error {
				server.StartCleanupLoop(runCtx)
				return nil
			})
			return err
		},
		OnStop: func(stopCtx context.Context) error {
			return cleanupRunner.Stop(stopCtx)
		},
	})
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

func createEdgeMTLSIssueEventsInternal(ctx context.Context, eventService *event.EventService, envID string, envName string, remoteAddr string, certIssued bool, caGenerated bool, reenrolled bool) {
	if eventService == nil {
		return
	}
	if caGenerated {
		_, _ = eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:        models.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    models.EventSeverityInfo,
			Title:       "Edge mTLS CA generated",
			Description: "Arcane generated a new edge mTLS certificate authority",
			Metadata:    models.JSON{"remoteAddr": remoteAddr, "kind": "ca"},
		})
	}
	if certIssued {
		_, _ = eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          models.EventTypeEnvironmentMTLSCertIssued,
			Severity:      edgeMTLSCertIssuedSeverityInternal(reenrolled),
			Title:         "Edge mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an edge mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
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

// handleEdgeStatusChange records a tunnel up/down transition: it updates the
// stored connection state, logs an event when the state actually changed, and
// wakes any open status streams.
func handleEdgeStatusChange(ctx context.Context, environmentService *environment.EnvironmentService, eventService *event.EventService, envID string, connected bool) {
	envName := envID
	env, getErr := environmentService.GetEnvironmentByID(ctx, envID)
	if getErr != nil {
		slog.WarnContext(ctx, "Failed to load environment before edge status update", "environment_id", envID, "error", getErr)
	} else if env != nil && env.Name != "" {
		envName = env.Name
	}

	if err := environmentService.UpdateEnvironmentConnectionState(ctx, envID, connected); err != nil {
		slog.WarnContext(ctx, "Failed to update environment status on edge connect/disconnect", "environment_id", envID, "connected", connected, "error", err)
	} else {
		slog.InfoContext(ctx, "Updated edge environment connection state", "environment_id", envID, "connected", connected)
	}

	// Only log an event on an actual state transition; poll-mode tunnels can
	// re-register without the environment ever having gone offline (session
	// replacement, transport reconnects), and those are not worth an event.
	alreadyInState := env != nil &&
		((connected && env.Status == string(models.EnvironmentStatusOnline)) ||
			(!connected && env.Status == string(models.EnvironmentStatusOffline)))
	if !alreadyInState {
		if err := createEdgeConnectionEvent(ctx, eventService, envID, envName, connected); err != nil {
			slog.WarnContext(ctx, "Failed to create edge connection event", "environment_id", envID, "connected", connected, "error", err)
		}
	}

	// This is the only funnel for "an edge tunnel came up or went down"
	// (register, unregister and stale reaping all route through it), so it
	// is where open status streams learn about it without polling.
	environmentService.NotifyRuntimeStateChanged()
}

func createEdgeConnectionEvent(ctx context.Context, eventService *event.EventService, envID, envName string, connected bool) error {
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

	_, err := eventService.CreateEvent(ctx, event.CreateEventRequest{
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
		return errors.WrapIf(err, "failed to create edge lifecycle event")
	}

	return nil
}
