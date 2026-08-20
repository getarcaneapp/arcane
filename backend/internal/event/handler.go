package event

import (
	"context"
	"crypto/subtle"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"strings"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	eventtypes "github.com/getarcaneapp/arcane/types/v2/event"
	"github.com/labstack/echo/v5"
)

// EventHandler handles event management endpoints.
type EventHandler struct {
	eventService *EventService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListEventsInput struct {
	Search   string `query:"search" doc:"Search query"`
	Sort     string `query:"sort" doc:"Column to sort by"`
	Order    string `query:"order" default:"asc" doc:"Sort direction"`
	Start    int    `query:"start" default:"0" doc:"Start index"`
	Limit    int    `query:"limit" default:"20" doc:"Limit"`
	Severity string `query:"severity" doc:"Filter by severity"`
	Type     string `query:"type" doc:"Filter by event type (exact type or category prefix, comma-separated)"`
}

type GetEventStatsInput struct{}

type GetEventsByEnvironmentInput struct {
	EnvironmentID string `path:"environmentId" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
	Severity      string `query:"severity" doc:"Filter by severity"`
	Type          string `query:"type" doc:"Filter by event type (exact type or category prefix, comma-separated)"`
}

type DeleteEventInput struct {
	EventID string `path:"eventId" doc:"Event ID"`
}

// ============================================================================
// Registration
// ============================================================================

// RegisterAgentEventIngestion registers the manager ingestion endpoint used by
// direct agents when no edge tunnel is active. This route is not part of the
// Huma/OpenAPI surface and authenticates only with the configured agent token.
func RegisterAgentEventIngestion(g *echo.Group, eventService *EventService, cfg *config.Config) {
	g.POST("/events", func(c *echo.Context) error {
		if eventService == nil {
			return c.JSON(http.StatusInternalServerError, base.ApiResponse[base.MessageResponse]{
				Success: false,
				Data:    base.MessageResponse{Message: "service not available"},
			})
		}
		if cfg == nil || strings.TrimSpace(cfg.AgentToken) == "" {
			slog.Warn("agent event ingestion is disabled because agent token is not configured")
			return c.JSON(http.StatusServiceUnavailable, base.ApiResponse[base.MessageResponse]{
				Success: false,
				Data:    base.MessageResponse{Message: "agent event ingestion is not configured"},
			})
		}
		if !validAgentEventIngestionTokenInternal(c.Request(), cfg) {
			return c.JSON(http.StatusUnauthorized, base.ApiResponse[base.MessageResponse]{
				Success: false,
				Data:    base.MessageResponse{Message: "invalid agent token"},
			})
		}

		var input CreateEventRequest
		if err := json.UnmarshalRead(http.MaxBytesReader(c.Response(), c.Request().Body, 1<<20), &input); err != nil {
			return c.JSON(http.StatusBadRequest, base.ApiResponse[base.MessageResponse]{
				Success: false,
				Data:    base.MessageResponse{Message: "invalid event payload"},
			})
		}
		if strings.TrimSpace(string(input.Type)) == "" || strings.TrimSpace(input.Title) == "" {
			return c.JSON(http.StatusBadRequest, base.ApiResponse[base.MessageResponse]{
				Success: false,
				Data:    base.MessageResponse{Message: "event type and title are required"},
			})
		}

		if _, err := eventService.CreateEvent(c.Request().Context(), input); err != nil {
			return c.JSON(http.StatusInternalServerError, base.ApiResponse[base.MessageResponse]{
				Success: false,
				Data:    base.MessageResponse{Message: errors.WithMessage(err, "Failed to create event").Error()},
			})
		}

		return c.JSON(http.StatusAccepted, base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "event ingested"},
		})
	})
}

func validAgentEventIngestionTokenInternal(r *http.Request, cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	token := r.Header.Get(utils.HeaderAgentToken)
	if token == "" || cfg.AgentToken == "" {
		return false
	}
	// Constant time, matching auth.AgentTokenMatches in the auth middleware:
	// this endpoint is unauthenticated apart from the token, so a byte-by-byte
	// comparison is directly probeable.
	return subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AgentToken)) == 1
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
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListEvents returns a paginated list of events.
func (h *EventHandler) ListEvents(ctx context.Context, input *ListEventsInput) (*handlerutil.Page[eventtypes.Event], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	events, paginationResp, err := h.eventService.ListEventsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list events").Error())
	}

	return &handlerutil.Page[eventtypes.Event]{
		Body: base.Paginated[eventtypes.Event]{
			Success:    true,
			Data:       events,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetEventStats returns global event counts grouped by severity.
func (h *EventHandler) GetEventStats(ctx context.Context, _ *GetEventStatsInput) (*handlerutil.Out[EventSeverityCounts], error) {
	counts, err := h.eventService.GetEventSeverityCounts(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to load event statistics").Error())
	}

	return &handlerutil.Out[EventSeverityCounts]{
		Body: base.ApiResponse[EventSeverityCounts]{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// GetEventsByEnvironment returns events for a specific environment.
func (h *EventHandler) GetEventsByEnvironment(ctx context.Context, input *GetEventsByEnvironmentInput) (*handlerutil.Page[eventtypes.Event], error) {
	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest("Environment ID is required")
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	events, paginationResp, err := h.eventService.GetEventsByEnvironmentPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list events").Error())
	}

	return &handlerutil.Page[eventtypes.Event]{
		Body: base.Paginated[eventtypes.Event]{
			Success:    true,
			Data:       events,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// DeleteEvent deletes an event.
func (h *EventHandler) DeleteEvent(ctx context.Context, input *DeleteEventInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.EventID == "" {
		return nil, huma.Error400BadRequest("Event ID is required")
	}

	if err := h.eventService.DeleteEvent(ctx, input.EventID); err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to delete event").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Event deleted successfully",
			},
		},
	}, nil
}
