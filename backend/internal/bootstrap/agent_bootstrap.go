package bootstrap

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	kit "go.getarcane.app/kit/pkg"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
)

// registerAgentTunnelRoutes configures the manager-side agent tunnel server.
// It registers the WebSocket route and prepares gRPC service state on the shared listener.
// Returns the TunnelServer for graceful shutdown.
func registerAgentTunnelRoutes(
	ctx context.Context,
	lifecycle fx.Lifecycle,
	cfg *config.Config,
	apiGroup *echo.Group,
	environmentService *environment.EnvironmentService,
	eventService *event.EventService,
	notificationService *notification.NotificationService,
	registry *agent.TunnelRegistry,
) *agent.TunnelServer {
	server := agent.NewTunnelServer(agent.TunnelServerOptions{
		Registry: registry,
		Resolver: environmentService.ResolveAgentEnvironmentByToken,
		StatusCallback: func(ctx context.Context, envID string, connected bool) {
			handleAgentStatusChange(ctx, environmentService, eventService, envID, connected)
		},
		Config: &agent.Config{
			AgentMTLSMode:       cfg.AgentMTLSMode,
			AgentMTLSCAFile:     cfg.AgentMTLSCAFile,
			AgentMTLSCertFile:   cfg.AgentMTLSCertFile,
			AgentMTLSKeyFile:    cfg.AgentMTLSKeyFile,
			AgentMTLSServerName: cfg.AgentMTLSServerName,
			AgentMTLSAssetsDir:  cfg.AgentMTLSAssetsDir,
			AppURL:              cfg.GetAppURL(),
			ManagerApiUrl:       cfg.ManagerApiUrl,
		},
		NameResolver: func(ctx context.Context, envID string) (string, error) {
			env, err := environmentService.GetEnvironmentByID(ctx, envID)
			if err != nil {
				return "", err
			}
			return env.Name, nil
		},
		EventCallback: func(ctx context.Context, envID string, evt *agent.TunnelEvent) error {
			return handleAgentEvent(ctx, envID, evt, eventService, notificationService)
		},
		EnrollmentCallback: func(ctx context.Context, envID, remoteAddr string, certIssued, caGenerated, reenrolled bool) {
			handleAgentEnrollment(ctx, environmentService, eventService, envID, remoteAddr, certIssued, caGenerated, reenrolled)
		},
	})

	var stopCleanup func(context.Context) error

	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var err error
			stopCleanup, err = concurrency.StartSupervised(ctx, "agent tunnel cleanup", func(runCtx context.Context) error {
				server.StartCleanupLoop(runCtx)
				return nil
			})
			return err
		},
		OnStop: func(stopCtx context.Context) error {
			if stopCleanup == nil {
				return nil
			}
			return stopCleanup(stopCtx)
		},
	})

	// Rate-limit agent mTLS enrollment per-IP. Enrollment is authenticated
	// only by the agent token, so we cap bursts to mitigate brute-force or
	// token-abuse attempts without impacting normal agent lifecycles.
	apiGroup.POST("/tunnel/mtls/enroll", server.HandleMTLSEnroll, middleware.PerIPRateLimit(10, 3), middleware.PerAgentTokenRateLimit(10, 3))
	apiGroup.GET("/tunnel/connect", server.HandleConnect, middleware.PerIPRateLimit(60, 30), middleware.PerAgentTokenRateLimit(10, 3))
	slog.InfoContext(ctx, "Configured agent tunnel server", "grpcEnabled", !cfg.AgentEnabled, "websocketEnabled", true)
	return server
}

func handleAgentEnrollment(
	ctx context.Context,
	environmentService *environment.EnvironmentService,
	eventService *event.EventService,
	envID, remoteAddr string,
	certIssued, caGenerated, reenrolled bool,
) {
	if eventService == nil {
		return
	}

	env, err := environmentService.GetEnvironmentByID(ctx, envID)
	if err != nil {
		env = &environment.Environment{}
	}

	_, _ = eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeEnvironmentMTLSEnroll,
		Severity:      kit.Ternary(reenrolled, event.EventSeverityWarning, event.EventSeverityInfo),
		Title:         "Agent mTLS enrollment",
		Description:   "Agent completed mTLS enrollment from " + remoteAddr,
		ResourceType:  new("environment"),
		ResourceID:    &envID,
		ResourceName:  &env.Name,
		EnvironmentID: &envID,
		Metadata:      database.JSON{"remoteAddr": remoteAddr, "reenrollment": reenrolled},
	})
	createAgentMTLSIssueEvents(ctx, eventService, envID, env.Name, remoteAddr, certIssued, caGenerated, reenrolled)
}

func createAgentMTLSIssueEvents(ctx context.Context, eventService *event.EventService, envID, envName, remoteAddr string, certIssued, caGenerated, reenrolled bool) {
	if caGenerated {
		_, _ = eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:        event.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    event.EventSeverityInfo,
			Title:       "Agent mTLS CA generated",
			Description: "Arcane generated a new agent mTLS certificate authority",
			Metadata:    database.JSON{"remoteAddr": remoteAddr, "kind": "ca"},
		})
	}
	if certIssued {
		_, _ = eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeEnvironmentMTLSCertIssued,
			Severity:      kit.Ternary(reenrolled, event.EventSeverityWarning, event.EventSeverityInfo),
			Title:         "Agent mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an agent mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
			ResourceID:    &envID,
			ResourceName:  &envName,
			EnvironmentID: &envID,
			Metadata:      database.JSON{"remoteAddr": remoteAddr, "kind": "client", "reenrollment": reenrolled},
		})
	}
}

// handleAgentStatusChange records a tunnel up/down transition: it updates the
// stored connection state, logs an event when the state actually changed, and
// wakes any open status streams.
func handleAgentStatusChange(ctx context.Context, environmentService *environment.EnvironmentService, eventService *event.EventService, envID string, connected bool) {
	env, getErr := environmentService.GetEnvironmentByID(ctx, envID)
	if getErr != nil {
		slog.WarnContext(ctx, "Failed to load environment before agent status update", "environmentId", envID, "error", getErr)
		env = &environment.Environment{}
	}
	envName := kit.Ternary(env.Name != "", env.Name, envID)

	if err := environmentService.UpdateEnvironmentConnectionState(ctx, envID, connected); err != nil {
		slog.WarnContext(ctx, "Failed to update environment status on agent connect/disconnect", "environmentId", envID, "connected", connected, "error", err)
	} else {
		slog.InfoContext(ctx, "Updated agent environment connection state", "environmentId", envID, "connected", connected)
	}

	targetStatus := kit.Ternary(connected, environment.EnvironmentStatusOnline, environment.EnvironmentStatusOffline)
	if env.Status != string(targetStatus) {
		if err := createAgentConnectionEvent(ctx, eventService, envID, envName, connected); err != nil {
			slog.WarnContext(ctx, "Failed to create agent connection event", "environmentId", envID, "connected", connected, "error", err)
		}
	}

	// This is the only funnel for "an agent tunnel came up or went down"
	// (register, unregister and stale reaping all route through it)
	// so it is where open status streams learn about it without polling.
	environmentService.NotifyRuntimeStateChanged()
}

func createAgentConnectionEvent(ctx context.Context, eventService *event.EventService, envID, envName string, connected bool) error {
	if eventService == nil {
		return nil
	}

	eventType := event.EventTypeEnvironmentDisconnect
	title := "Agent Disconnected"
	description := fmt.Sprintf("Agent for environment '%s' disconnected", envName)
	severity := event.EventSeverityWarning

	if connected {
		eventType = event.EventTypeEnvironmentConnect
		title = "Agent Connected"
		description = fmt.Sprintf("Agent for environment '%s' connected", envName)
		severity = event.EventSeveritySuccess
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
		return fmt.Errorf("failed to create agent lifecycle event: %w", err)
	}

	return nil
}

func handleAgentEvent(ctx context.Context, envID string, evt *agent.TunnelEvent, eventService *event.EventService, notificationService *notification.NotificationService) error {
	if evt == nil {
		return errors.New("event payload is required")
	}

	if evt.Type == agent.TunnelEventTypeNotificationDispatch {
		if notificationService == nil {
			return errors.New("notification service is not available for agent dispatch")
		}
		var payload types.NotificationDispatchRequest
		if err := json.Unmarshal(evt.MetadataJSON, &payload); err != nil {
			return fmt.Errorf("failed to decode agent notification dispatch payload: %w", err)
		}
		_, err := notificationService.DispatchNotificationForEnvironment(ctx, envID, payload)
		if err != nil {
			return fmt.Errorf("failed to dispatch agent notification: %w", err)
		}
		return nil
	}

	var metadata database.JSON
	if len(evt.MetadataJSON) > 0 {
		metadata = database.JSON{}
		if err := json.Unmarshal(evt.MetadataJSON, &metadata); err != nil {
			return fmt.Errorf("failed to decode event metadata: %w", err)
		}
	}

	_, err := eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventType(evt.Type),
		Severity:      event.EventSeverity(evt.Severity),
		Title:         evt.Title,
		Description:   evt.Description,
		ResourceType:  new(evt.ResourceType),
		ResourceID:    new(evt.ResourceID),
		ResourceName:  new(evt.ResourceName),
		UserID:        new(evt.UserID),
		Username:      new(evt.Username),
		EnvironmentID: &envID,
		Metadata:      metadata,
	})
	if err != nil {
		return fmt.Errorf("failed to persist synced event: %w", err)
	}
	return nil
}
