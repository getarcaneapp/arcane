package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/activity"
	"github.com/getarcaneapp/arcane/types/base"
	"gorm.io/gorm"
)

type ActivityHandler struct {
	activityService    *services.ActivityService
	environmentService *services.EnvironmentService
}

type ListActivitiesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"desc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"50" doc:"Limit"`
	Status        string `query:"status" doc:"Filter by activity status"`
	Type          string `query:"type" doc:"Filter by activity type"`
	ResourceType  string `query:"resourceType" doc:"Filter by resource type"`
}

type ListActivitiesOutput struct {
	Body base.Paginated[activity.Activity]
}

type GetActivityInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ActivityID    string `path:"activityId" doc:"Activity ID"`
	Limit         int    `query:"limit" default:"500" doc:"Maximum messages to return"`
}

type GetActivityOutput struct {
	Body base.ApiResponse[activity.Detail]
}

type ClearActivityHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ClearActivityHistoryOutput struct {
	Body base.ApiResponse[activity.ClearHistoryResult]
}

type StreamActivitiesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Limit         int    `query:"limit" default:"50" doc:"Initial snapshot limit"`
}

func RegisterActivities(api huma.API, activityService *services.ActivityService, environmentService *services.EnvironmentService) {
	h := &ActivityHandler{
		activityService:    activityService,
		environmentService: environmentService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-activities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities",
		Summary:     "List background activities",
		Description: "Get current and recent background activities for an environment",
		Tags:        []string{"Activities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermActivitiesRead),
	}, h.ListActivities)

	huma.Register(api, huma.Operation{
		OperationID: "get-activity",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities/{activityId}",
		Summary:     "Get background activity",
		Description: "Get a background activity with its recent output messages",
		Tags:        []string{"Activities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermActivitiesRead),
	}, h.GetActivity)

	huma.Register(api, huma.Operation{
		OperationID: "stream-activities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities/stream",
		Summary:     "Stream background activities",
		Description: "Stream background activity updates as JSON lines",
		Tags:        []string{"Activities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermActivitiesRead),
	}, h.StreamActivities)

	huma.Register(api, huma.Operation{
		OperationID: "clear-activity-history",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/activities/history",
		Summary:     "Clear background activity history",
		Description: "Delete completed background activity history for an environment",
		Tags:        []string{"Activities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermActivitiesDelete),
	}, h.ClearHistory)
}

func (h *ActivityHandler) ListActivities(ctx context.Context, input *ListActivitiesInput) (*ListActivitiesOutput, error) {
	if input.EnvironmentID != "0" {
		return h.proxyListActivitiesInternal(ctx, input)
	}
	if h.activityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}
	if input.ResourceType != "" {
		params.Filters["resourceType"] = input.ResourceType
	}

	activities, paginationResp, err := h.activityService.ListActivitiesPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.applyActivitySourceLabelsInternal(ctx, input.EnvironmentID, activities)

	return &ListActivitiesOutput{
		Body: base.Paginated[activity.Activity]{
			Success: true,
			Data:    activities,
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

func (h *ActivityHandler) GetActivity(ctx context.Context, input *GetActivityInput) (*GetActivityOutput, error) {
	if input.EnvironmentID != "0" {
		return h.proxyGetActivityInternal(ctx, input)
	}
	if h.activityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.ActivityID == "" {
		return nil, huma.Error400BadRequest("activity id is required")
	}

	detail, err := h.activityService.GetActivityDetail(ctx, input.EnvironmentID, input.ActivityID, input.Limit)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("activity not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, &detail.Activity)

	return &GetActivityOutput{
		Body: base.ApiResponse[activity.Detail]{
			Success: true,
			Data:    *detail,
		},
	}, nil
}

func (h *ActivityHandler) StreamActivities(ctx context.Context, input *StreamActivitiesInput) (*huma.StreamResponse, error) {
	if h.activityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // streaming work must use humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/x-json-stream")
			humaCtx.SetHeader("Cache-Control", "no-cache")
			humaCtx.SetHeader("Connection", "keep-alive")
			humaCtx.SetHeader("X-Accel-Buffering", "no")

			writer := humaCtx.BodyWriter()
			encoder := json.NewEncoder(writer)
			flush := func() {
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
			}

			if input.EnvironmentID != "0" {
				h.streamRemoteActivitySnapshotsInternal(humaCtx.Context(), input, encoder, flush)
				return
			}

			h.streamLocalActivitiesInternal(humaCtx.Context(), input, encoder, flush)
		},
	}, nil
}

func (h *ActivityHandler) streamLocalActivitiesInternal(
	ctx context.Context,
	input *StreamActivitiesInput,
	encoder *json.Encoder,
	flush func(),
) {
	sendSnapshot := func() bool {
		activities, _, err := h.activityService.ListActivitiesPaginated(ctx, input.EnvironmentID, pagination.QueryParams{
			PaginationParams: pagination.PaginationParams{Limit: resolveActivityStreamLimitInternal(input.Limit)},
		})
		if err != nil {
			return false
		}
		h.applyActivitySourceLabelsInternal(ctx, input.EnvironmentID, activities)
		if err := encoder.Encode(activity.StreamEvent{
			Type:       "snapshot",
			Activities: activities,
			Timestamp:  time.Now(),
		}); err != nil {
			return false
		}
		flush()
		return true
	}
	if !sendSnapshot() {
		return
	}

	events, missedEvents, unsubscribe := h.activityService.Subscribe(input.EnvironmentID)
	defer unsubscribe()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			h.applyActivityStreamEventSourceLabelInternal(ctx, input.EnvironmentID, &event)
			if err := encoder.Encode(event); err != nil {
				return
			}
			flush()
		case <-ticker.C:
			if missedEvents() && !sendSnapshot() {
				return
			}
			if err := encoder.Encode(activity.StreamEvent{
				Type:      "heartbeat",
				Timestamp: time.Now(),
			}); err != nil {
				return
			}
			flush()
		}
	}
}

func (h *ActivityHandler) ClearHistory(ctx context.Context, input *ClearActivityHistoryInput) (*ClearActivityHistoryOutput, error) {
	if input.EnvironmentID != "0" {
		return h.proxyClearHistoryInternal(ctx, input)
	}
	if h.activityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	deleted, err := h.activityService.DeleteHistory(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &ClearActivityHistoryOutput{
		Body: base.ApiResponse[activity.ClearHistoryResult]{
			Success: true,
			Data:    activity.ClearHistoryResult{Deleted: deleted},
		},
	}, nil
}

func (h *ActivityHandler) streamRemoteActivitySnapshotsInternal(
	ctx context.Context,
	input *StreamActivitiesInput,
	encoder *json.Encoder,
	flush func(),
) {
	pollTicker := time.NewTicker(5 * time.Second)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	sendSnapshot := func(ctx context.Context) bool {
		output, err := h.proxyListActivitiesInternal(ctx, &ListActivitiesInput{
			EnvironmentID: input.EnvironmentID,
			Start:         0,
			Limit:         resolveActivityStreamLimitInternal(input.Limit),
			Order:         "desc",
		})
		if err != nil {
			return false
		}
		if err := encoder.Encode(activity.StreamEvent{
			Type:       "snapshot",
			Activities: output.Body.Data,
			Timestamp:  time.Now(),
		}); err != nil {
			return false
		}
		flush()
		return true
	}

	if !sendSnapshot(ctx) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			if !sendSnapshot(ctx) {
				return
			}
		case <-heartbeatTicker.C:
			if err := encoder.Encode(activity.StreamEvent{
				Type:      "heartbeat",
				Timestamp: time.Now(),
			}); err != nil {
				return
			}
			flush()
		}
	}
}

func (h *ActivityHandler) proxyListActivitiesInternal(ctx context.Context, input *ListActivitiesInput) (*ListActivitiesOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}
	path := "/api/environments/0/activities?" + activityListQueryInternal(input).Encode()
	out, err := proxyRemoteJSONInternal[base.Paginated[activity.Activity]](ctx, h.environmentService, input.EnvironmentID, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	h.applyActivitySourceLabelsInternal(ctx, input.EnvironmentID, out.Data)
	return &ListActivitiesOutput{Body: *out}, nil
}

func (h *ActivityHandler) proxyGetActivityInternal(ctx context.Context, input *GetActivityInput) (*GetActivityOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}
	path := fmt.Sprintf("/api/environments/0/activities/%s?limit=%d", url.PathEscape(input.ActivityID), input.Limit)
	out, err := proxyRemoteJSONInternal[base.ApiResponse[activity.Detail]](ctx, h.environmentService, input.EnvironmentID, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, &out.Data.Activity)
	return &GetActivityOutput{Body: *out}, nil
}

func (h *ActivityHandler) proxyClearHistoryInternal(ctx context.Context, input *ClearActivityHistoryInput) (*ClearActivityHistoryOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}
	out, err := proxyRemoteJSONInternal[base.ApiResponse[activity.ClearHistoryResult]](ctx, h.environmentService, input.EnvironmentID, http.MethodDelete, "/api/environments/0/activities/history", nil)
	if err != nil {
		return nil, err
	}
	return &ClearActivityHistoryOutput{Body: *out}, nil
}

func (h *ActivityHandler) applyActivitySourceLabelsInternal(ctx context.Context, environmentID string, activities []activity.Activity) {
	sourceID, sourceName := h.resolveActivitySourceInternal(ctx, environmentID)
	for i := range activities {
		applyActivitySourceInternal(&activities[i], sourceID, sourceName)
	}
}

func (h *ActivityHandler) applyActivitySourceLabelInternal(ctx context.Context, environmentID string, item *activity.Activity) {
	sourceID, sourceName := h.resolveActivitySourceInternal(ctx, environmentID)
	applyActivitySourceInternal(item, sourceID, sourceName)
}

func (h *ActivityHandler) applyActivityStreamEventSourceLabelInternal(ctx context.Context, environmentID string, event *activity.StreamEvent) {
	if event == nil {
		return
	}
	sourceID, sourceName := h.resolveActivitySourceInternal(ctx, environmentID)
	if event.Activity != nil {
		applyActivitySourceInternal(event.Activity, sourceID, sourceName)
	}
	for i := range event.Activities {
		applyActivitySourceInternal(&event.Activities[i], sourceID, sourceName)
	}
}

func (h *ActivityHandler) resolveActivitySourceInternal(ctx context.Context, environmentID string) (string, string) {
	if environmentID == "" {
		environmentID = "0"
	}
	if h.environmentService != nil {
		if env, err := h.environmentService.GetEnvironmentByID(ctx, environmentID); err == nil && env != nil {
			return env.ID, env.Name
		}
	}
	if environmentID == "0" {
		return "0", "Local"
	}
	return environmentID, environmentID
}

func applyActivitySourceInternal(item *activity.Activity, sourceID, sourceName string) {
	if item == nil {
		return
	}
	item.SourceEnvironmentID = sourceID
	item.SourceEnvironmentName = sourceName
}

func activityListQueryInternal(input *ListActivitiesInput) url.Values {
	values := url.Values{}
	values.Set("start", strconv.Itoa(input.Start))
	values.Set("limit", strconv.Itoa(input.Limit))
	if input.Search != "" {
		values.Set("search", input.Search)
	}
	if input.Sort != "" {
		values.Set("sort", input.Sort)
	}
	if input.Order != "" {
		values.Set("order", input.Order)
	}
	if input.Status != "" {
		values.Set("status", input.Status)
	}
	if input.Type != "" {
		values.Set("type", input.Type)
	}
	if input.ResourceType != "" {
		values.Set("resourceType", input.ResourceType)
	}
	return values
}

func resolveActivityStreamLimitInternal(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}
