package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/event"
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
	Page    int    `query:"pagination[page]" default:"1" doc:"Page number"`
	Limit   int    `query:"pagination[limit]" default:"20" doc:"Items per page"`
	SortCol string `query:"sort[column]" doc:"Column to sort by"`
	SortDir string `query:"sort[direction]" default:"asc" doc:"Sort direction"`
}

type ListEventsOutput struct {
	Body EventPaginatedResponse
}

type GetEventsByEnvironmentInput struct {
	EnvironmentID string `path:"environmentId" doc:"Environment ID"`
	Page          int    `query:"pagination[page]" default:"1" doc:"Page number"`
	Limit         int    `query:"pagination[limit]" default:"20" doc:"Items per page"`
	SortCol       string `query:"sort[column]" doc:"Column to sort by"`
	SortDir       string `query:"sort[direction]" default:"asc" doc:"Sort direction"`
}

type GetEventsByEnvironmentOutput struct {
	Body EventPaginatedResponse
}

type CreateEventInput struct {
	Body event.CreateEvent
}

type CreateEventOutput struct {
	Body base.ApiResponse[event.Event]
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

// RegisterEvents registers all event management endpoints.
func RegisterEvents(api huma.API, eventService *services.EventService) {
	h := &EventHandler{eventService: eventService}

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
	}, h.ListEvents)

	huma.Register(api, huma.Operation{
		OperationID: "createEvent",
		Method:      "POST",
		Path:        "/events",
		Summary:     "Create an event",
		Description: "Create a new system event",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateEvent)

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

	params := buildPaginationParams(input.Page, input.Limit, input.SortCol, input.SortDir)

	events, paginationResp, err := h.eventService.ListEventsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EventListError{Err: err}).Error())
	}

	return &ListEventsOutput{
		Body: EventPaginatedResponse{
			Success: true,
			Data:    events,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
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

	params := buildPaginationParams(input.Page, input.Limit, input.SortCol, input.SortDir)

	events, paginationResp, err := h.eventService.GetEventsByEnvironmentPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EventListError{Err: err}).Error())
	}

	return &GetEventsByEnvironmentOutput{
		Body: EventPaginatedResponse{
			Success: true,
			Data:    events,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// CreateEvent creates a new event.
func (h *EventHandler) CreateEvent(ctx context.Context, input *CreateEventInput) (*CreateEventOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	evt, err := h.eventService.CreateEventFromDto(ctx, input.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EventCreationError{Err: err}).Error())
	}

	return &CreateEventOutput{
		Body: base.ApiResponse[event.Event]{
			Success: true,
			Data:    *evt,
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
