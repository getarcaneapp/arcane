package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/event"
)

// EventHandler handles event management endpoints.
type EventHandler struct {
	eventService  *services.EventService
	apiKeyService *services.ApiKeyService
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
	Search       string `query:"search" doc:"Search query"`
	Sort         string `query:"sort" doc:"Column to sort by"`
	Order        string `query:"order" default:"desc" doc:"Sort direction"`
	Start        int    `query:"start" default:"0" doc:"Start index"`
	Limit        int    `query:"limit" default:"20" doc:"Limit"`
	Severity     string `query:"severity" doc:"Filter by severity"`
	Type         string `query:"type" doc:"Filter by event type"`
	ResourceType string `query:"resourceType" doc:"Filter by resource type"`
	Username     string `query:"username" doc:"Filter by username"`
	Environment  string `query:"environmentId" doc:"Filter by environment ID"`
}

type ListEventsOutput struct {
	Body EventPaginatedResponse
}

type GetEventsByEnvironmentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"desc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
	Severity      string `query:"severity" doc:"Filter by severity"`
	Type          string `query:"type" doc:"Filter by event type"`
	ResourceType  string `query:"resourceType" doc:"Filter by resource type"`
	Username      string `query:"username" doc:"Filter by username"`
}

type GetEventsByEnvironmentOutput struct {
	Body EventPaginatedResponse
}

// Legacy route input: keep old path parameter name for backward compatibility.
type GetEventsByEnvironmentLegacyInput struct {
	EnvironmentID string `path:"environmentId" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"desc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
	Severity      string `query:"severity" doc:"Filter by severity"`
	Type          string `query:"type" doc:"Filter by event type"`
	ResourceType  string `query:"resourceType" doc:"Filter by resource type"`
	Username      string `query:"username" doc:"Filter by username"`
}

type DeleteEventByEnvironmentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	EventID       string `path:"eventId" doc:"Event ID"`
}

type DeleteEventByEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
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

type SyncEventsInput struct {
	XAPIKey string `header:"X-API-Key" doc:"API key for agent authentication"`
	Body    event.SyncEventsRequest
}

type SyncEventsOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterEvents registers all event management endpoints.
func RegisterEvents(api huma.API, eventService *services.EventService, apiKeyService *services.ApiKeyService) {
	h := &EventHandler{
		eventService:  eventService,
		apiKeyService: apiKeyService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "listEvents",
		Method:      http.MethodGet,
		Path:        "/events",
		Summary:     "List events",
		Description: "Get a paginated list of system events",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListEvents)

	// Environment-scoped events (proxied to remote environments by EnvironmentMiddleware).
	huma.Register(api, huma.Operation{
		OperationID: "listEventsByEnvironment",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/events",
		Summary:     "List events by environment",
		Description: "Get a paginated list of events for a specific environment",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetEventsByEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "createEvent",
		Method:      http.MethodPost,
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
		Method:      http.MethodDelete,
		Path:        "/events/{eventId}",
		Summary:     "Delete an event",
		Description: "Delete a system event by ID",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteEvent)

	// Environment-scoped delete (proxied for remote environments).
	huma.Register(api, huma.Operation{
		OperationID: "deleteEventByEnvironment",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/events/{eventId}",
		Summary:     "Delete an event in an environment",
		Description: "Delete an event by ID within the selected environment",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteEventByEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "getEventsByEnvironment",
		Method:      http.MethodGet,
		Path:        "/events/environment/{environmentId}",
		Summary:     "Get events by environment",
		Description: "Get a paginated list of events for a specific environment",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetEventsByEnvironmentLegacy)

	huma.Register(api, huma.Operation{
		OperationID: "syncEventsFromAgent",
		Method:      http.MethodPost,
		Path:        "/events/sync",
		Summary:     "Sync events from agent",
		Description: "Receive and store events from a remote agent",
		Tags:        []string{"Events"},
		Security: []map[string][]string{
			{"ApiKeyAuth": {}},
		},
	}, h.SyncEvents)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListEvents returns a paginated list of events.
func (h *EventHandler) ListEvents(ctx context.Context, input *ListEventsInput) (*ListEventsOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: input.Search},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
		Filters: map[string]string{},
	}
	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}
	if input.ResourceType != "" {
		params.Filters["resourceType"] = input.ResourceType
	}
	if input.Username != "" {
		params.Filters["username"] = input.Username
	}
	if input.Environment != "" {
		params.Filters["environmentId"] = input.Environment
	}

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

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: input.Search},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
		Filters: map[string]string{},
	}
	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}
	if input.ResourceType != "" {
		params.Filters["resourceType"] = input.ResourceType
	}
	if input.Username != "" {
		params.Filters["username"] = input.Username
	}

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

// GetEventsByEnvironmentLegacy supports the previous /events/environment/{environmentId} route.
func (h *EventHandler) GetEventsByEnvironmentLegacy(ctx context.Context, input *GetEventsByEnvironmentLegacyInput) (*GetEventsByEnvironmentOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest((&common.EnvironmentIDRequiredError{}).Error())
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: input.Search},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
		Filters: map[string]string{},
	}
	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}
	if input.ResourceType != "" {
		params.Filters["resourceType"] = input.ResourceType
	}
	if input.Username != "" {
		params.Filters["username"] = input.Username
	}

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

func (h *EventHandler) DeleteEventByEnvironment(ctx context.Context, input *DeleteEventByEnvironmentInput) (*DeleteEventByEnvironmentOutput, error) {
	if h.eventService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.EventID == "" {
		return nil, huma.Error400BadRequest((&common.EventIDRequiredError{}).Error())
	}
	if err := h.eventService.DeleteEvent(ctx, input.EventID); err != nil {
		return nil, huma.Error500InternalServerError((&common.EventDeletionError{Err: err}).Error())
	}
	return &DeleteEventByEnvironmentOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Event deleted successfully"},
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

// SyncEvents receives events from a remote agent and stores them.
func (h *EventHandler) SyncEvents(ctx context.Context, input *SyncEventsInput) (*SyncEventsOutput, error) {
	if h.eventService == nil || h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if len(input.Body.Events) == 0 {
		return &SyncEventsOutput{
			Body: base.ApiResponse[base.MessageResponse]{
				Success: true,
				Data:    base.MessageResponse{Message: "No events to sync"},
			},
		}, nil
	}

	// Get environment ID from API key
	envID, err := h.apiKeyService.GetEnvironmentByApiKey(ctx, input.XAPIKey)
	if err != nil || envID == nil {
		return nil, huma.Error401Unauthorized("Invalid or missing API key")
	}

	slog.InfoContext(ctx, "Received event sync from agent",
		"eventCount", len(input.Body.Events),
		"environmentID", *envID)

	// Override environment ID in all events with the one from the API key
	for i := range input.Body.Events {
		input.Body.Events[i].EnvironmentID = *envID
	}

	err = h.eventService.SyncEventsFromAgent(ctx, input.Body.Events)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to sync events from agent", "error", err)
		return nil, huma.Error500InternalServerError((&common.EventCreationError{Err: err}).Error())
	}

	slog.InfoContext(ctx, "Successfully synced events from agent", "eventCount", len(input.Body.Events))

	return &SyncEventsOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Events synced successfully"},
		},
	}, nil
}
