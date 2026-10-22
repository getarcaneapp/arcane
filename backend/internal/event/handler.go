package event

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// EventHandler handles event management endpoints.
type EventHandler struct {
	eventService *EventService
}

type ListEventsInput struct {
	Search       string `query:"search" doc:"Search query"`
	Sort         string `query:"sort" doc:"Column to sort by"`
	Order        string `query:"order" default:"asc" doc:"Sort direction"`
	Start        int    `query:"start" default:"0" doc:"Start index"`
	Limit        int    `query:"limit" default:"20" doc:"Limit"`
	Severity     string `query:"severity" doc:"Filter by severity"`
	Type         string `query:"type" doc:"Filter by event type (exact type or category prefix, comma-separated)"`
	IncludeTotal bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
}

type ExportEventsInput struct {
	Format        string `query:"format" enum:"csv,ndjson" default:"csv" doc:"Export file format"`
	Search        string `query:"search" doc:"Search query"`
	Severity      string `query:"severity" doc:"Filter by severity"`
	Type          string `query:"type" doc:"Filter by event type (exact type or category prefix, comma-separated)"`
	EnvironmentID string `query:"environmentId" doc:"Only events for this environment"`
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
	IncludeTotal  bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
}

type WaitForEnvironmentChangesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Epoch         string `query:"epoch" doc:"Epoch from the previous response; a different epoch returns immediately"`
	Since         uint64 `query:"since" doc:"Highest kind revision already seen; waits until a newer revision exists"`
	Wait          uint64 `query:"wait" doc:"Maximum seconds to wait for a newer revision; capped at the server limit"`
}

type DeleteEventInput struct {
	EventID string `path:"eventId" doc:"Event ID"`
}

// ListEvents returns a paginated list of events.
func (h *EventHandler) ListEvents(ctx context.Context, input *ListEventsInput) (*handlerutil.Page[types.Event], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.SkipCount = !input.IncludeTotal

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

	return &handlerutil.Page[types.Event]{
		Body: types.Paginated[types.Event]{
			Success:    true,
			Data:       events,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetEventStats returns global event counts grouped by severity.
func (h *EventHandler) GetEventStats(ctx context.Context, _ *GetEventStatsInput) (*handlerutil.Out[types.EventSeverityCounts], error) {
	counts, err := h.eventService.GetEventSeverityCounts(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load event statistics: " + err.Error())
	}

	return &handlerutil.Out[types.EventSeverityCounts]{
		Body: types.ApiResponse[types.EventSeverityCounts]{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// ExportEvents streams matching events, oldest first, as CSV or NDJSON.
func (h *EventHandler) ExportEvents(ctx context.Context, input *ExportEventsInput) (*huma.StreamResponse, error) {
	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: input.Search},
		Filters: map[string]string{
			"severity":      input.Severity,
			"type":          input.Type,
			"environmentId": input.EnvironmentID,
		},
	}
	filename := fmt.Sprintf("events-%s.%s", time.Now().UTC().Format("2006-01-02T15-04-05Z"), input.Format)
	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			humaCtx.SetHeader("Content-Type", kit.Ternary(input.Format == types.EventExportFormatCSV, "text/csv; charset=utf-8", "application/x-ndjson"))
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
			if err := h.eventService.ExportEvents(ctx, params, input.Format, humaCtx.BodyWriter()); err != nil {
				slog.ErrorContext(ctx, "Failed to stream event export", "format", input.Format, "error", err)
			}
		},
	}, nil
}

// GetEventsByEnvironment returns events for a specific environment.
func (h *EventHandler) GetEventsByEnvironment(ctx context.Context, input *GetEventsByEnvironmentInput) (*handlerutil.Page[types.Event], error) {
	if input.EnvironmentID == "" {
		return nil, huma.Error400BadRequest("Environment ID is required")
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.SkipCount = !input.IncludeTotal

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

	return &handlerutil.Page[types.Event]{
		Body: types.Paginated[types.Event]{
			Success:    true,
			Data:       events,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// DeleteEvent deletes an event.
func (h *EventHandler) DeleteEvent(ctx context.Context, input *DeleteEventInput) (*handlerutil.Out[types.MessageResponse], error) {
	if input.EventID == "" {
		return nil, huma.Error400BadRequest("Event ID is required")
	}

	if err := h.eventService.DeleteEvent(ctx, input.EventID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete event: "+apiErr.Message)
	}

	return handlerutil.MessageOutput("Event deleted successfully", ""), nil
}

func (h *EventHandler) WaitForEnvironmentChanges(ctx context.Context, input *WaitForEnvironmentChangesInput) (*handlerutil.Out[types.EnvironmentChanges], error) {
	return &handlerutil.Out[types.EnvironmentChanges]{
		Body: types.ApiResponse[types.EnvironmentChanges]{
			Success: true,
			Data:    h.eventService.WaitForChanges(ctx, input.EnvironmentID, input.Epoch, input.Since, input.Wait),
		},
	}, nil
}
