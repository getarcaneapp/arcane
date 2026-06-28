package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/aggstream"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
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

type StreamAllActivitiesInput struct {
	Limit int `query:"limit" default:"50" doc:"Snapshot limit per environment"`
}

const (
	activityStreamHeartbeatInterval    = 15 * time.Second
	activityStreamRemotePollInterval   = 5 * time.Second
	activityStreamEnvReconcileInterval = 30 * time.Second
	activityStreamRemotePollTimeout    = 15 * time.Second
	activityStreamEventBuffer          = 256
)

type CancelActivityInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ActivityID    string `path:"activityId" doc:"Activity ID"`
	RequestedBy   string `query:"requestedBy" doc:"Display name to attribute the cancellation to (used when proxying to a remote environment)"`
}

type CancelActivityOutput struct {
	Body base.ApiResponse[activity.Activity]
}

func RegisterActivities(api huma.API, activityService *services.ActivityService, environmentService *services.EnvironmentService) {
	h := &ActivityHandler{
		activityService:    activityService,
		environmentService: environmentService,
	}

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermActivitiesRead, h.ListActivities)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermActivitiesRead, h.GetActivity)

	huma.Register(api, huma.Operation{
		OperationID: "stream-all-activities",
		Method:      http.MethodGet,
		Path:        "/activities/stream",
		Summary:     "Stream background activities across all environments",
		Description: "Stream background activity updates for the local environment and all enabled remote environments as JSON lines",
		Tags:        []string{"Activities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermActivitiesRead),
	}, h.StreamAllActivities)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "cancel-activity",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/activities/{activityId}/cancel",
		Summary:     "Cancel a background activity",
		Description: "Request cancellation of a running or queued background activity",
		Tags:        []string{"Activities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermActivitiesCancel, h.CancelActivity)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermActivitiesDelete, h.ClearHistory)
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
			Success:    true,
			Data:       activities,
			Pagination: toPaginationResponseInternal(paginationResp),
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

func (h *ActivityHandler) CancelActivity(ctx context.Context, input *CancelActivityInput) (*CancelActivityOutput, error) {
	if input.EnvironmentID != "0" {
		return h.proxyCancelActivityInternal(ctx, input)
	}
	if h.activityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.ActivityID == "" {
		return nil, huma.Error400BadRequest("activity id is required")
	}

	requestedBy := h.cancelRequestedByInternal(ctx, input.RequestedBy)
	cancelled, err := h.activityService.CancelActivity(ctx, input.EnvironmentID, input.ActivityID, requestedBy)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, huma.Error404NotFound("activity not found")
		case errors.Is(err, services.ErrActivityNotCancelable):
			return nil, huma.Error409Conflict("activity is not running")
		default:
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, cancelled)

	return &CancelActivityOutput{
		Body: base.ApiResponse[activity.Activity]{
			Success: true,
			Data:    *cancelled,
		},
	}, nil
}

func (h *ActivityHandler) proxyCancelActivityInternal(ctx context.Context, input *CancelActivityInput) (*CancelActivityOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}
	path := fmt.Sprintf("/api/environments/0/activities/%s/cancel", url.PathEscape(input.ActivityID))
	if requestedBy := h.cancelRequestedByInternal(ctx, input.RequestedBy); requestedBy != "" {
		path += "?requestedBy=" + url.QueryEscape(requestedBy)
	}
	out, err := proxyRemoteJSONInternal[base.ApiResponse[activity.Activity]](ctx, h.environmentService, input.EnvironmentID, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, &out.Data)
	return &CancelActivityOutput{Body: *out}, nil
}

// cancelRequestedByInternal resolves a human-readable name for the cancellation
// audit message, preferring the authenticated user and falling back to a name
// forwarded from a proxying controller.
func (h *ActivityHandler) cancelRequestedByInternal(ctx context.Context, forwarded string) string {
	if user, ok := humamw.GetCurrentUserFromContext(ctx); ok && user != nil {
		if user.DisplayName != nil && strings.TrimSpace(*user.DisplayName) != "" {
			return strings.TrimSpace(*user.DisplayName)
		}
		if name := strings.TrimSpace(user.Username); name != "" {
			return name
		}
	}
	return strings.TrimSpace(forwarded)
}

func (h *ActivityHandler) StreamAllActivities(ctx context.Context, input *StreamAllActivitiesInput) (*huma.StreamResponse, error) {
	if h.activityService == nil || h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // streaming work must use humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			writer := humaCtx.BodyWriter()
			encoder := json.NewEncoder(writer)
			flush := func() {
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
			}

			h.streamAllActivitiesInternal(humaCtx.Context(), input.Limit, encoder, flush)
		},
	}, nil
}

// streamAllActivitiesInternal multiplexes activity events for the local
// environment and every enabled remote environment over a single response so
// the browser needs one connection regardless of environment count.
func (h *ActivityHandler) streamAllActivitiesInternal(ctx context.Context, limit int, encoder *json.Encoder, flush func()) {
	aggstream.Run(ctx, encoder, flush, activityStreamEventBuffer, activityStreamHeartbeatInterval,
		func() activity.StreamEvent {
			return activity.StreamEvent{Type: "heartbeat", Timestamp: time.Now()}
		},
		func(ctx context.Context, events chan<- activity.StreamEvent) {
			h.runLocalActivityStreamProducerInternal(ctx, limit, events)
		},
		func(ctx context.Context, events chan<- activity.StreamEvent) {
			h.runRemoteActivityStreamPollersInternal(ctx, limit, events)
		},
	)
}

func (h *ActivityHandler) runLocalActivityStreamProducerInternal(ctx context.Context, limit int, events chan<- activity.StreamEvent) {
	sendSnapshot := func() bool {
		activities, _, err := h.activityService.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{
			Params: pagination.Params{Limit: resolveActivityStreamLimitInternal(limit)},
		})
		if err != nil {
			if ctx.Err() == nil {
				aggstream.Send(ctx, events, activity.StreamEvent{
					Type:          "error",
					EnvironmentID: "0",
					Error:         err.Error(),
					Timestamp:     time.Now(),
				})
			}
			return false
		}
		h.applyActivitySourceLabelsInternal(ctx, "0", activities)
		return aggstream.Send(ctx, events, activity.StreamEvent{
			Type:          "snapshot",
			EnvironmentID: "0",
			Activities:    activities,
			Timestamp:     time.Now(),
		})
	}

	snapshotOK := sendSnapshot()

	eventCh, missedEvents, unsubscribe := h.activityService.Subscribe("0")
	defer unsubscribe()

	ticker := time.NewTicker(activityStreamHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			event.EnvironmentID = "0"
			h.applyActivityStreamEventSourceLabelInternal(ctx, "0", &event)
			if !aggstream.Send(ctx, events, event) {
				return
			}
		case <-ticker.C:
			if !snapshotOK || missedEvents() {
				snapshotOK = sendSnapshot()
			}
		}
	}
}

// runRemoteActivityStreamPollersInternal keeps one poller goroutine per
// enabled remote environment, re-listing periodically so environments added
// or removed while the stream is open are picked up without a reconnect.
func (h *ActivityHandler) runRemoteActivityStreamPollersInternal(ctx context.Context, limit int, events chan<- activity.StreamEvent) {
	aggstream.ReconcilePollersByKey(ctx,
		h.environmentService.ListRemoteEnvironments,
		func(environment models.Environment) string {
			return environment.ID
		},
		activityStreamEnvironmentVersionInternal,
		activityStreamEnvReconcileInterval,
		"activity stream",
		func(pollCtx context.Context, environment models.Environment) {
			h.runRemoteActivityStreamPollerInternal(pollCtx, environment, limit, events)
		})
}

func activityStreamEnvironmentVersionInternal(environment models.Environment) string {
	if environment.UpdatedAt == nil {
		return environment.ID
	}
	return environment.ID + ":" + environment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func (h *ActivityHandler) runRemoteActivityStreamPollerInternal(ctx context.Context, environment models.Environment, limit int, events chan<- activity.StreamEvent) {
	environmentID := environment.ID
	lastError := ""

	poll := func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, activityStreamRemotePollTimeout)
		defer cancelPoll()

		currentEnvironment := environment
		if h.environmentService != nil {
			var ok bool
			currentEnvironment, ok = h.environmentService.GetActiveRemoteEnvironmentSnapshot(environmentID)
			if !ok {
				return
			}
		}

		output, err := h.proxyListActivitiesForEnvironmentInternal(pollCtx, currentEnvironment, &ListActivitiesInput{
			EnvironmentID: environmentID,
			Limit:         resolveActivityStreamLimitInternal(limit),
			Order:         "desc",
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A failing environment must not end the stream; surface the error
			// once per distinct message and keep polling.
			if msg := err.Error(); msg != lastError {
				lastError = msg
				aggstream.Send(ctx, events, activity.StreamEvent{
					Type:          "error",
					EnvironmentID: environmentID,
					Error:         msg,
					Timestamp:     time.Now(),
				})
			}
			return
		}
		lastError = ""
		aggstream.Send(ctx, events, activity.StreamEvent{
			Type:          "snapshot",
			EnvironmentID: environmentID,
			Activities:    output.Body.Data,
			Timestamp:     time.Now(),
		})
	}

	poll()

	ticker := time.NewTicker(activityStreamRemotePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
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

func (h *ActivityHandler) proxyListActivitiesForEnvironmentInternal(ctx context.Context, environment models.Environment, input *ListActivitiesInput) (*ListActivitiesOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}
	path := "/api/environments/0/activities?" + activityListQueryInternal(input).Encode()
	var out base.Paginated[activity.Activity]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, path, nil, &out); err != nil {
		return nil, translateRemoteProxyErrorInternal(err)
	}
	applyActivitySourceLabelsForEnvironmentInternal(environment, out.Data)
	return &ListActivitiesOutput{Body: out}, nil
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

func applyActivitySourceLabelsForEnvironmentInternal(environment models.Environment, activities []activity.Activity) {
	sourceID, sourceName := activitySourceFromEnvironmentInternal(environment)
	for i := range activities {
		applyActivitySourceInternal(&activities[i], sourceID, sourceName)
	}
}

func activitySourceFromEnvironmentInternal(environment models.Environment) (string, string) {
	environmentID := environment.ID
	if environmentID == "" {
		environmentID = "0"
	}
	environmentName := environment.Name
	if environmentName == "" {
		if environmentID == "0" {
			environmentName = "Local"
		} else {
			environmentName = environmentID
		}
	}
	return environmentID, environmentName
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
