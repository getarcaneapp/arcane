package bootstrap

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/notification"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

type TunnelServerParams struct {
	fx.In

	Context             context.Context
	Lifecycle           fx.Lifecycle
	Config              *config.Config
	Registry            *agent.TunnelRegistry
	MTLS                *agent.MTLS
	EnvironmentService  *environment.EnvironmentService
	EventService        *event.EventService
	NotificationService *notification.NotificationService
	SettingsService     *settings.SettingsService
}

func newTunnelServer(p TunnelServerParams) *agent.TunnelServer {
	if p.Config.AgentEnabled {
		return nil
	}
	server := agent.NewTunnelServer(agent.TunnelServerOptions{
		Registry: p.Registry,
		Resolver: p.EnvironmentService.ResolveAgentEnvironmentByToken,
		StatusCallback: func(ctx context.Context, envID string, connected bool) {
			handleAgentStatusChange(ctx, p.EnvironmentService, p.EventService, envID, connected)
		},
		MTLS: p.MTLS,
		EventCallback: func(ctx context.Context, envID string, evt *agent.TunnelEvent) error {
			return handleAgentEvent(ctx, envID, evt, p.EventService, p.NotificationService)
		},
		StreamWindowMiB: func() int {
			return p.SettingsService.GetSettingsOrDefaults(p.Context).AgentTunnelStreamWindowMb.AsInt()
		},
	})

	var stopCleanup func(context.Context) error

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			stopCleanup = concurrency.Supervise(p.Context, concurrency.SuperviseConfig{
				Run: func(runCtx context.Context) error {
					server.StartCleanupLoop(runCtx)
					return nil
				},
				Name: "agent tunnel cleanup",
			}).Stop
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			if stopCleanup == nil {
				return nil
			}
			return stopCleanup(stopCtx)
		},
	})

	slog.InfoContext(p.Context, "Configured agent tunnel server", "grpcEnabled", true, "websocketEnabled", true)
	return server
}

func tunnelRoutes(server *agent.TunnelServer) []middleware.EchoRoute {
	return []middleware.EchoRoute{{
		Method:      http.MethodGet,
		Path:        "/tunnel/connect",
		Access:      authz.EndpointAccessAgentToken,
		Handler:     server.HandleConnect,
		Middlewares: []echo.MiddlewareFunc{middleware.PerIPRateLimit(60, 30), middleware.PerAgentTokenRateLimit(10, 3)},
	}}
}

// handleAgentStatusChange stores the tunnel state, logs real transitions, and wakes status streams.
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

	// Every tunnel up/down path funnels here, so status streams learn about it without polling.
	environmentService.NotifyRuntimeStateChanged()
}

func createAgentConnectionEvent(ctx context.Context, eventService *event.EventService, envID, envName string, connected bool) error {
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
