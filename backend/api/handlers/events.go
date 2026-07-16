package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/event"
	"github.com/labstack/echo/v4"
)

// EventHandler handles event management endpoints.
type EventHandler struct {
	eventService *services.EventService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// EventPaginatedResponse is the paginated response for events.
type EventPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []event.Event           `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListEventsInput struct {
	Search   string `query:"search" doc:"Search query"`
	Sort     string `query:"sort" doc:"Column to sort by"`
	Order    string `query:"order" default:"asc" doc:"Sort direction"`
	Start    int    `query:"start" default:"0" doc:"Start index"`
	Limit    int    `query:"limit" default:"20" doc:"Limit"`
	Severity string `query:"severity" doc:"Filter by severity"`
	Type     string `query:"type" doc:"Filter by event type (exact type or category prefix, comma-separated)"`
}

type ListEventsOutput struct {
	Body EventPaginatedResponse
}

type GetEventStatsInput struct{}

// EventStatsResponse is a dedicated response type to avoid schema name collision.
type EventStatsResponse struct {
	Success bool                         `json:"success"`
	Data    services.EventSeverityCounts `json:"data"`
}

type GetEventStatsOutput struct {
	Body EventStatsResponse
}

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

type GetEventsByEnvironmentOutput struct {
	Body EventPaginatedResponse
}

type DeleteEventInput struct {
	EventID string `path:"eventId" doc:"Event ID"`
}

type DeleteEventOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterAgentEventIngestion registers the manager ingestion endpoint used by
// direct agents when no edge tunnel is active. This route is not part of the
// Huma/OpenAPI surface and authenticates only with the configured agent token.
func RegisterAgentEventIngestion(g *echo.Group, eventService *services.EventService, cfg *config.Config) {
	g.POST("/events", func(c echo.Context) error {
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

		var input services.CreateEventRequest
		decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 1<<20))
		if err := decoder.Decode(&input); err != nil {
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
				Data:    base.MessageResponse{Message: (&common.EventCreationError{Err: err}).Error()},
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
	token := r.Header.Get(pkgutils.HeaderAgentToken)
	return token != "" && token == cfg.AgentToken
}

// RegisterEvents registers all event management endpoints.
func RegisterEvents(api huma.API, eventService *services.EventService) {
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
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEventsRead),
	}, h.ListEvents)

	huma.Register(api, huma.Operation{
		OperationID: "getEventStats",
		Method:      "GET",
		Path:        "/events/stats",
		Summary:     "Event severity counts",
		Description: "Get global event counts grouped by severity",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEventsRead),
	}, h.GetEventStats)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEvent",
		Method:      "DELETE",
		Path:        "/events/{eventId}",
		Summary:     "Delete an event",
		Description: "Delete a system event by ID",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEventsDelete),
	}, h.DeleteEvent)

	huma.Register(api, huma.Operation{
		OperationID: "getEventsByEnvironment",
		Method:      "GET",
		Path:        "/events/environment/{environmentId}",
		Summary:     "Get events by environment",
		Description: "Get a paginated list of events for a specific environment",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEventsRead),
	}, h.GetEventsByEnvironment)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListEvents returns a paginated list of events.
func (h *EventHandler) ListEvents(ctx context.Context, input *ListEventsInput) (*ListEventsOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	events, paginationResp, err := h.eventService.ListEventsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EventListError{Err: err}).Error())
	}

	return &ListEventsOutput{
		Body: EventPaginatedResponse{
			Success:    true,
			Data:       events,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetEventStats returns global event counts grouped by severity.
func (h *EventHandler) GetEventStats(ctx context.Context, _ *GetEventStatsInput) (*GetEventStatsOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	counts, err := h.eventService.GetEventSeverityCounts(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EventStatsError{Err: err}).Error())
	}

	return &GetEventStatsOutput{
		Body: EventStatsResponse{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// GetEventsByEnvironment returns events for a specific environment.
func (h *EventHandler) GetEventsByEnvironment(ctx context.Context, input *GetEventsByEnvironmentInput) (*GetEventsByEnvironmentOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest((&common.EnvironmentIDRequiredError{}).Error())
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	events, paginationResp, err := h.eventService.GetEventsByEnvironmentPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EventListError{Err: err}).Error())
	}

	return &GetEventsByEnvironmentOutput{
		Body: EventPaginatedResponse{
			Success:    true,
			Data:       events,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// DeleteEvent deletes an event.
func (h *EventHandler) DeleteEvent(ctx context.Context, input *DeleteEventInput) (*DeleteEventOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EventID == "" {
		return nil, huma.Error400BadRequest((&common.EventIDRequiredError{}).Error())
	}

	if err := h.eventService.DeleteEvent(ctx, input.EventID); err != nil {
		return nil, huma.Error500InternalServerError((&common.EventDeletionError{Err: err}).Error())
	}

	return &DeleteEventOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Event deleted successfully",
			},
		},
	}, nil
}
