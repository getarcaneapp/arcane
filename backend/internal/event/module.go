// Package event owns persisted system events, manager ingestion, and event HTTP routes.
package event

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Module owns event persistence and both of the domain's HTTP surfaces.
type Module struct {
	service *EventService
}

// New assembles the event routes around its service.
func New(service *EventService) *Module {
	return &Module{service: service}
}

// Service exposes event operations to collaborating domains.
func (m *Module) Service() *EventService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the typed event endpoints for runtime and schema discovery.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterEvents(api, nil)
		return
	}
	RegisterEvents(api, m.service)
}

// RegisterAgentRoutes mounts the token-authenticated agent event ingestion endpoint.
func (m *Module) RegisterAgentRoutes(group *echo.Group, resolveEnvironment func(context.Context, string) (string, error)) {
	if m == nil {
		RegisterAgentEventIngestion(group, nil, nil)
		return
	}
	RegisterAgentEventIngestion(group, m.service, resolveEnvironment)
}

// RegisterAgentEventIngestion registers the manager ingestion endpoint agent
// agents fall back to over HTTP when no tunnel is active. This route is not part of the
// Huma/OpenAPI surface and authenticates with the originating environment token.
func RegisterAgentEventIngestion(g *echo.Group, eventService *EventService, resolveEnvironment func(context.Context, string) (string, error)) {
	g.POST("/events", func(c *echo.Context) error {
		if eventService == nil {
			return c.JSON(http.StatusInternalServerError, types.ApiResponse[types.MessageResponse]{
				Success: false,
				Data:    types.MessageResponse{Message: "service not available"},
			})
		}

		if resolveEnvironment == nil {
			return c.JSON(http.StatusServiceUnavailable, types.ApiResponse[types.MessageResponse]{
				Success: false,
				Data:    types.MessageResponse{Message: "agent event ingestion is not configured"},
			})
		}
		environmentID, err := resolveEnvironment(c.Request().Context(), c.Request().Header.Get(middleware.HeaderAgentToken))
		if err != nil || environmentID == "" || environmentID == "0" {
			return c.JSON(http.StatusUnauthorized, types.ApiResponse[types.MessageResponse]{
				Success: false,
				Data:    types.MessageResponse{Message: "invalid agent token"},
			})
		}

		var input CreateEventRequest
		if unmarshalReadErr := json.UnmarshalRead(http.MaxBytesReader(c.Response(), c.Request().Body, 1<<20), &input); unmarshalReadErr != nil {
			return c.JSON(http.StatusBadRequest, types.ApiResponse[types.MessageResponse]{
				Success: false,
				Data:    types.MessageResponse{Message: "invalid event payload"},
			})
		}
		if strings.TrimSpace(string(input.Type)) == "" || strings.TrimSpace(input.Title) == "" {
			return c.JSON(http.StatusBadRequest, types.ApiResponse[types.MessageResponse]{
				Success: false,
				Data:    types.MessageResponse{Message: "event type and title are required"},
			})
		}

		if _, ingestAgentEventErr := eventService.IngestAgentEvent(c.Request().Context(), environmentID, input); ingestAgentEventErr != nil {
			return c.JSON(http.StatusInternalServerError, types.ApiResponse[types.MessageResponse]{
				Success: false,
				Data:    types.MessageResponse{Message: "Failed to create event: " + ingestAgentEventErr.Error()},
			})
		}

		return c.JSON(http.StatusAccepted, types.ApiResponse[types.MessageResponse]{
			Success: true,
			Data:    types.MessageResponse{Message: "event ingested"},
		})
	})
}

// RegisterEvents registers all event management endpoints.
func RegisterEvents(api huma.API, eventService *EventService) {
	h := &EventHandler{
		eventService: eventService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "listEvents",
		Method:      "GET",
		Path:        "/events",
		Summary:     "List events",
		Description: "Get a paginated list of system events",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEventsRead),
	}, h.ListEvents)

	huma.Register(api, huma.Operation{
		OperationID: "getEventStats",
		Method:      "GET",
		Path:        "/events/stats",
		Summary:     "Event severity counts",
		Description: "Get global event counts grouped by severity",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEventsRead),
	}, h.GetEventStats)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEvent",
		Method:      "DELETE",
		Path:        "/events/{eventId}",
		Summary:     "Delete an event",
		Description: "Delete a system event by ID",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEventsDelete),
	}, h.DeleteEvent)

	huma.Register(api, huma.Operation{
		OperationID: "getEventsByEnvironment",
		Method:      "GET",
		Path:        "/events/environment/{environmentId}",
		Summary:     "Get events by environment",
		Description: "Get a paginated list of events for a specific environment",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEventsRead),
	}, h.GetEventsByEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "waitForEnvironmentChanges",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/events/changes",
		Summary:     "Wait for environment changes",
		Description: "Long-poll the latest revision of each changed resource kind in an environment",
		Tags:        []string{"Events"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEventsRead, h.WaitForEnvironmentChanges)
}
