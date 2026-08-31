package activity

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/samber/mo"
	"go.getarcane.app/streams/agg"
	"gorm.io/gorm"
)

type ActivityHandler struct {
	activityService *ActivityService
	environment     EnvironmentDependencies

	// remoteStreamHub shares one poller per remote environment across every
	// connected stream client instead of polling per client × environment.
	remoteStreamHub *agg.Hub[activitytypes.StreamEvent]
}

type EnvironmentDependencies struct {
	ProxyJSONRequest               handlerutil.RemoteJSONProxy
	ListRemoteEnvironments         func(context.Context) ([]environment.Environment, error)
	GetActiveRemoteEnvironment     func(string) mo.Option[environment.Environment]
	ProxyJSONRequestForEnvironment func(context.Context, environment.Environment, string, string, []byte, any) error
	ResolveEnvironmentName         func(context.Context, string) string
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

type GetActivityInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ActivityID    string `path:"activityId" doc:"Activity ID"`
	Limit         int    `query:"limit" default:"500" doc:"Maximum messages to return"`
}

type ClearActivityHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type StreamAllActivitiesInput struct {
	Limit int `query:"limit" default:"50" doc:"Snapshot limit per environment"`
}

const (
	activityStreamHeartbeatInterval    = 15 * time.Second
	activityStreamRemotePollInterval   = 5 * time.Second
	activityStreamEnvReconcileInterval = 30 * time.Second
	activityStreamRemotePollTimeout    = 15 * time.Second
)

type CancelActivityInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ActivityID    string `path:"activityId" doc:"Activity ID"`
	RequestedBy   string `query:"requestedBy" doc:"Display name to attribute the cancellation to (used when proxying to a remote environment)"`
}

func NewHandler(activityService *ActivityService, environment EnvironmentDependencies) *ActivityHandler {
	return &ActivityHandler{
		activityService: activityService,
		environment:     environment,
		remoteStreamHub: agg.NewHub[activitytypes.StreamEvent](),
	}
}

func RegisterActivities(api huma.API, h *ActivityHandler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-activities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities",
		Summary:     "List background activities",
		Description: "Get current and recent background activities for an environment",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesRead, h.ListActivities)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-activity",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/activities/{activityId}",
		Summary:     "Get background activity",
		Description: "Get a background activity with its recent output messages",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesRead, h.GetActivity)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "cancel-activity",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/activities/{activityId}/cancel",
		Summary:     "Cancel a background activity",
		Description: "Request cancellation of a running or queued background activity",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesCancel, h.CancelActivity)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "clear-activity-history",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/activities/history",
		Summary:     "Clear background activity history",
		Description: "Delete completed background activity history for an environment",
		Tags:        []string{"Activities"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermActivitiesDelete, h.ClearHistory)
}

func (h *ActivityHandler) ListActivities(ctx context.Context, input *ListActivitiesInput) (*handlerutil.Page[activitytypes.Activity], error) {
	if input.EnvironmentID != "0" {
		return h.proxyListActivitiesInternal(ctx, input)
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
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

	return &handlerutil.Page[activitytypes.Activity]{
		Body: base.Paginated[activitytypes.Activity]{
			Success:    true,
			Data:       activities,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *ActivityHandler) GetActivity(ctx context.Context, input *GetActivityInput) (*handlerutil.Out[activitytypes.Detail], error) {
	if input.EnvironmentID != "0" {
		return h.proxyGetActivityInternal(ctx, input)
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

	return &handlerutil.Out[activitytypes.Detail]{
		Body: base.ApiResponse[activitytypes.Detail]{
			Success: true,
			Data:    *detail,
		},
	}, nil
}

func (h *ActivityHandler) ClearHistory(ctx context.Context, input *ClearActivityHistoryInput) (*handlerutil.Out[activitytypes.ClearHistoryResult], error) {
	if input.EnvironmentID != "0" {
		return h.proxyClearHistoryInternal(ctx, input)
	}

	deleted, err := h.activityService.DeleteHistory(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[activitytypes.ClearHistoryResult]{
		Body: base.ApiResponse[activitytypes.ClearHistoryResult]{
			Success: true,
			Data:    activitytypes.ClearHistoryResult{Deleted: deleted},
		},
	}, nil
}

func (h *ActivityHandler) CancelActivity(ctx context.Context, input *CancelActivityInput) (*handlerutil.Out[activitytypes.Activity], error) {
	if input.EnvironmentID != "0" {
		return h.proxyCancelActivityInternal(ctx, input)
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
		case errors.Is(err, ErrActivityNotCancelable):
			return nil, huma.Error409Conflict("activity is not running")
		default:
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, cancelled)

	return &handlerutil.Out[activitytypes.Activity]{
		Body: base.ApiResponse[activitytypes.Activity]{
			Success: true,
			Data:    *cancelled,
		},
	}, nil
}

func (h *ActivityHandler) proxyCancelActivityInternal(ctx context.Context, input *CancelActivityInput) (*handlerutil.Out[activitytypes.Activity], error) {
	path := fmt.Sprintf("/api/environments/0/activities/%s/cancel", url.PathEscape(input.ActivityID))
	if requestedBy := h.cancelRequestedByInternal(ctx, input.RequestedBy); requestedBy != "" {
		path += "?requestedBy=" + url.QueryEscape(requestedBy)
	}
	out, err := h.environment.ProxyJSONRequest.JSON[base.ApiResponse[activitytypes.Activity]](ctx, input.EnvironmentID, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, &out.Data)
	return &handlerutil.Out[activitytypes.Activity]{Body: *out}, nil
}

// cancelRequestedByInternal resolves a human-readable name for the cancellation
// audit message, preferring the authenticated user and falling back to a name
// forwarded from a proxying controller.
func (h *ActivityHandler) cancelRequestedByInternal(ctx context.Context, forwarded string) string {
	if user, ok := common.CurrentUserFromContext(ctx); ok && user != nil {
		if user.DisplayName != nil && strings.TrimSpace(*user.DisplayName) != "" {
			return strings.TrimSpace(*user.DisplayName)
		}
		if name := strings.TrimSpace(user.Username); name != "" {
			return name
		}
	}
	return strings.TrimSpace(forwarded)
}

func (h *ActivityHandler) RunLocalStreamProducer(ctx context.Context, limit int, events chan<- activitytypes.StreamEvent) {
	sendSnapshot := func() bool {
		activities, _, err := h.activityService.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{
			Limit: resolveActivityStreamLimitInternal(limit),
		})
		if err != nil {
			if ctx.Err() == nil {
				agg.Send(ctx, events, activitytypes.StreamEvent{
					Type:          "error",
					EnvironmentID: "0",
					Error:         err.Error(),
					Timestamp:     time.Now(),
				})
			}
			return false
		}
		h.applyActivitySourceLabelsInternal(ctx, "0", activities)
		return agg.Send(ctx, events, activitytypes.StreamEvent{
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
			if !agg.Send(ctx, events, event) {
				return
			}
		case <-ticker.C:
			if !snapshotOK || missedEvents() {
				snapshotOK = sendSnapshot()
			}
		}
	}
}

// RunRemoteStreamPollers keeps one poller goroutine per
// enabled remote environment, re-listing periodically so environments added
// or removed while the stream is open are picked up without a reconnect.
func (h *ActivityHandler) RunRemoteStreamPollers(ctx context.Context, ps *authz.PermissionSet, limit int, events chan<- activitytypes.StreamEvent) {
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]environment.Environment, error) {
			environments, err := h.environment.ListRemoteEnvironments(ctx)
			if err != nil {
				return nil, err
			}
			allowed := environments[:0]
			for _, environment := range environments {
				if ps.Allows(authz.PermActivitiesRead, environment.ID) {
					allowed = append(allowed, environment)
				}
			}
			return allowed, nil
		},
		func(environment environment.Environment) string {
			return environment.ID
		},
		activityStreamEnvironmentVersionInternal,
		activityStreamEnvReconcileInterval,
		"activity stream",
		func(pollCtx context.Context, environment environment.Environment) {
			// One shared poller per environment (and snapshot limit) serves
			// every connected client; this subscriber only forwards its
			// events onto this client's stream.
			key := activityStreamEnvironmentVersionInternal(environment) + ":" + strconv.Itoa(resolveActivityStreamLimitInternal(limit))
			h.remoteStreamHub.Subscribe(pollCtx, key,
				func(runCtx context.Context, publish func(activitytypes.StreamEvent)) {
					h.runRemoteActivityStreamPollerInternal(runCtx, environment, limit, publish)
				},
				func(event activitytypes.StreamEvent) bool {
					return agg.Send(pollCtx, events, event)
				})
		})
}

func activityStreamEnvironmentVersionInternal(environment environment.Environment) string {
	if environment.UpdatedAt == nil {
		return environment.ID
	}
	return environment.ID + ":" + environment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

// activitySnapshotFingerprintInternal hashes the fields that affect what the
// client renders, so a poller can skip re-sending a snapshot identical to the
// previous one.
func activitySnapshotFingerprintInternal(items []activitytypes.Activity) string {
	hash := fnv.New64a()
	writeField := func(value string) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, item := range items {
		writeField(item.ID)
		writeField(string(item.Status))
		if item.Progress != nil {
			writeField(strconv.Itoa(*item.Progress))
		}
		writeField(item.Step)
		writeField(item.LatestMessage)
		if item.UpdatedAt != nil {
			writeField(item.UpdatedAt.UTC().Format(time.RFC3339Nano))
		}
		if item.EndedAt != nil {
			writeField(item.EndedAt.UTC().Format(time.RFC3339Nano))
		}
		writeField("|")
	}
	return strconv.FormatUint(hash.Sum64(), 16)
}

func (h *ActivityHandler) runRemoteActivityStreamPollerInternal(ctx context.Context, environment environment.Environment, limit int, publish func(activitytypes.StreamEvent)) {
	environmentID := environment.ID
	lastError := ""
	lastFingerprint := ""

	poll := func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, activityStreamRemotePollTimeout)
		defer cancelPoll()

		currentEnvironment := environment
		if h.environment.GetActiveRemoteEnvironment != nil {
			var ok bool
			currentEnvironment, ok = h.environment.GetActiveRemoteEnvironment(environmentID).Get()
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
				publish(activitytypes.StreamEvent{
					Type:          "error",
					EnvironmentID: environmentID,
					Error:         msg,
					Timestamp:     time.Now(),
				})
			}
			// Force a resync once the environment recovers.
			lastFingerprint = ""
			return
		}
		lastError = ""
		fingerprint := activitySnapshotFingerprintInternal(output.Body.Data)
		if fingerprint == lastFingerprint {
			return
		}
		publish(activitytypes.StreamEvent{
			Type:          "snapshot",
			EnvironmentID: environmentID,
			Activities:    output.Body.Data,
			Timestamp:     time.Now(),
		})
		lastFingerprint = fingerprint
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

func (h *ActivityHandler) proxyListActivitiesInternal(ctx context.Context, input *ListActivitiesInput) (*handlerutil.Page[activitytypes.Activity], error) {
	path := "/api/environments/0/activities?" + activityListQueryInternal(input).Encode()
	out, err := h.environment.ProxyJSONRequest.JSON[base.Paginated[activitytypes.Activity]](ctx, input.EnvironmentID, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	h.applyActivitySourceLabelsInternal(ctx, input.EnvironmentID, out.Data)
	return &handlerutil.Page[activitytypes.Activity]{Body: *out}, nil
}

func (h *ActivityHandler) proxyListActivitiesForEnvironmentInternal(ctx context.Context, environment environment.Environment, input *ListActivitiesInput) (*handlerutil.Page[activitytypes.Activity], error) {
	path := "/api/environments/0/activities?" + activityListQueryInternal(input).Encode()
	var out base.Paginated[activitytypes.Activity]
	if err := h.environment.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, path, nil, &out); err != nil {
		return nil, handlerutil.TranslateRemoteProxyError(err)
	}
	applyActivitySourceLabelsForEnvironmentInternal(environment, out.Data)
	return &handlerutil.Page[activitytypes.Activity]{Body: out}, nil
}

func (h *ActivityHandler) proxyGetActivityInternal(ctx context.Context, input *GetActivityInput) (*handlerutil.Out[activitytypes.Detail], error) {
	path := fmt.Sprintf("/api/environments/0/activities/%s?limit=%d", url.PathEscape(input.ActivityID), input.Limit)
	out, err := h.environment.ProxyJSONRequest.JSON[base.ApiResponse[activitytypes.Detail]](ctx, input.EnvironmentID, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	h.applyActivitySourceLabelInternal(ctx, input.EnvironmentID, &out.Data.Activity)
	return &handlerutil.Out[activitytypes.Detail]{Body: *out}, nil
}

func (h *ActivityHandler) proxyClearHistoryInternal(ctx context.Context, input *ClearActivityHistoryInput) (*handlerutil.Out[activitytypes.ClearHistoryResult], error) {
	out, err := h.environment.ProxyJSONRequest.JSON[base.ApiResponse[activitytypes.ClearHistoryResult]](ctx, input.EnvironmentID, http.MethodDelete, "/api/environments/0/activities/history", nil)
	if err != nil {
		return nil, err
	}
	return &handlerutil.Out[activitytypes.ClearHistoryResult]{Body: *out}, nil
}

func (h *ActivityHandler) applyActivitySourceLabelsInternal(ctx context.Context, environmentID string, activities []activitytypes.Activity) {
	sourceID, sourceName := h.resolveActivitySourceInternal(ctx, environmentID)
	for i := range activities {
		applyActivitySourceInternal(&activities[i], sourceID, sourceName)
	}
}

func (h *ActivityHandler) applyActivitySourceLabelInternal(ctx context.Context, environmentID string, item *activitytypes.Activity) {
	sourceID, sourceName := h.resolveActivitySourceInternal(ctx, environmentID)
	applyActivitySourceInternal(item, sourceID, sourceName)
}

func (h *ActivityHandler) applyActivityStreamEventSourceLabelInternal(ctx context.Context, environmentID string, event *activitytypes.StreamEvent) {
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

func applyActivitySourceLabelsForEnvironmentInternal(environmentModel environment.Environment, activities []activitytypes.Activity) {
	sourceID, sourceName := activitySourceFromEnvironmentInternal(environmentModel)
	for i := range activities {
		applyActivitySourceInternal(&activities[i], sourceID, sourceName)
	}
}

func activitySourceFromEnvironmentInternal(environmentModel environment.Environment) (string, string) {
	environmentID := environmentModel.ID
	if environmentID == "" {
		environmentID = environment.LocalEnvironmentID
	}
	return environmentID, environment.DisplayName(environmentID, environmentModel.Name)
}

func (h *ActivityHandler) resolveActivitySourceInternal(ctx context.Context, environmentID string) (string, string) {
	if environmentID == "" {
		environmentID = environment.LocalEnvironmentID
	}
	if h.environment.ResolveEnvironmentName != nil {
		return environmentID, h.environment.ResolveEnvironmentName(ctx, environmentID)
	}
	return environmentID, environment.DisplayName(environmentID, "")
}

func applyActivitySourceInternal(item *activitytypes.Activity, sourceID, sourceName string) {
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
