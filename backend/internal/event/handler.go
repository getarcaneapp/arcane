package event

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/event"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// EventHandler handles event management endpoints.
type EventHandler struct {
	eventService *EventService
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

// ListEvents returns a paginated list of events.
func (h *EventHandler) ListEvents(ctx context.Context, input *ListEventsInput) (*handlerutil.Page[event.Event], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	events, paginationResp, err := h.eventService.ListEventsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list events: " + err.Error())
	}

	return &handlerutil.Page[event.Event]{
		Body: base.Paginated[event.Event]{
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
		return nil, huma.Error500InternalServerError("Failed to load event statistics: " + err.Error())
	}

	return &handlerutil.Out[EventSeverityCounts]{
		Body: base.ApiResponse[EventSeverityCounts]{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// GetEventsByEnvironment returns events for a specific environment.
func (h *EventHandler) GetEventsByEnvironment(ctx context.Context, input *GetEventsByEnvironmentInput) (*handlerutil.Page[event.Event], error) {
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
		return nil, huma.Error500InternalServerError("Failed to list events: " + err.Error())
	}

	return &handlerutil.Page[event.Event]{
		Body: base.Paginated[event.Event]{
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
		return nil, huma.Error500InternalServerError("Failed to delete event: " + err.Error())
	}

	return handlerutil.MessageOutput("Event deleted successfully", ""), nil
}
