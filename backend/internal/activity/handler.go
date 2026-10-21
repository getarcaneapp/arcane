package activity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/streams/agg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

type ActivityHandler struct {
	activityService *ActivityService
	environment     EnvironmentDependencies
	remoteStreamHub *agg.Hub[types.ActivityStreamEvent]
}

type EnvironmentDependencies struct {
	ProxyJSONRequest               handlerutil.RemoteJSONProxy
	ListActiveRemoteEnvironments   func(context.Context) ([]environment.Environment, error)
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

func NewHandler(activityService *ActivityService, localEnvironment EnvironmentDependencies) *ActivityHandler {
	return &ActivityHandler{
		activityService: activityService,
		environment:     localEnvironment,
		remoteStreamHub: agg.NewHub[types.ActivityStreamEvent](),
	}
}

func (h *ActivityHandler) ListActivities(ctx context.Context, input *ListActivitiesInput) (*handlerutil.Page[types.Activity], error) {
	if input.EnvironmentID != "0" {
		unavailable := false
		out, err := h.listRemoteActivities(ctx, input, func(path string) (*types.Paginated[types.Activity], error) {
			var remote types.Paginated[types.Activity]
			if proxyErr := h.environment.ProxyJSONRequest(ctx, input.EnvironmentID, http.MethodGet, path, nil, &remote); proxyErr != nil {
				var transportErr *remenv.TransportError
				unavailable = errors.As(proxyErr, &transportErr) && transportErr.Unavailable()
				return nil, handlerutil.TranslateRemoteProxyError(proxyErr)
			}
			return &remote, nil
		})
		if out == nil {
			return nil, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err != nil && !unavailable {
			return nil, err
		}
		// HTTP responses must not discard stored summaries when the agent is offline.
		// Stream pollers retain the proxy error from the shared listing path.
		out.Body.Success = true
		return out, nil
	}

	params := activityListParams(input)

	activities, paginationResp, err := h.activityService.ListActivitiesPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.applyActivitySourceLabels(ctx, input.EnvironmentID, activities)

	return &handlerutil.Page[types.Activity]{
		Body: types.Paginated[types.Activity]{
			Success:    true,
			Data:       activities,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *ActivityHandler) GetActivity(ctx context.Context, input *GetActivityInput) (*handlerutil.Out[types.ActivityDetail], error) {
	if input.ActivityID == "" {
		return nil, huma.Error400BadRequest("activity id is required")
	}

	detail, err := h.activityService.GetActivityDetail(ctx, input.EnvironmentID, input.ActivityID, input.Limit)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.EnvironmentID != "0" {
				path := fmt.Sprintf("/api/environments/0/activities/%s?limit=%d", url.PathEscape(input.ActivityID), input.Limit)
				out, proxyErr := h.environment.ProxyJSONRequest.JSON[types.ApiResponse[types.ActivityDetail]](ctx, input.EnvironmentID, http.MethodGet, path, nil)
				if proxyErr != nil {
					return nil, proxyErr
				}
				h.applyActivitySourceLabel(ctx, input.EnvironmentID, &out.Data.Activity)
				return &handlerutil.Out[types.ActivityDetail]{Body: *out}, nil
			}
			return nil, huma.Error404NotFound("activity not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if !canReadJobActivity(ctx, detail.Activity) {
		return nil, huma.Error404NotFound("activity not found")
	}
	h.applyActivitySourceLabel(ctx, input.EnvironmentID, &detail.Activity)

	return &handlerutil.Out[types.ActivityDetail]{
		Body: types.ApiResponse[types.ActivityDetail]{
			Success: true,
			Data:    *detail,
		},
	}, nil
}

func (h *ActivityHandler) ClearHistory(ctx context.Context, input *ClearActivityHistoryInput) (*handlerutil.Out[types.ActivityClearHistoryResult], error) {
	deleted, err := h.activityService.DeleteHistory(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if input.EnvironmentID != "0" {
		out, proxyErr := h.environment.ProxyJSONRequest.JSON[types.ApiResponse[types.ActivityClearHistoryResult]](ctx, input.EnvironmentID, http.MethodDelete, "/api/environments/0/activities/history", nil)
		if proxyErr != nil {
			return nil, proxyErr
		}
		out.Data.Deleted += deleted
		return &handlerutil.Out[types.ActivityClearHistoryResult]{Body: *out}, nil
	}

	return &handlerutil.Out[types.ActivityClearHistoryResult]{
		Body: types.ApiResponse[types.ActivityClearHistoryResult]{
			Success: true,
			Data:    types.ActivityClearHistoryResult{Deleted: deleted},
		},
	}, nil
}

func (h *ActivityHandler) CancelActivity(ctx context.Context, input *CancelActivityInput) (*handlerutil.Out[types.Activity], error) {
	if input.ActivityID == "" {
		return nil, huma.Error400BadRequest("activity id is required")
	}

	// Prefer the authenticated user; a proxying manager forwards its user's name.
	requestedBy := strings.TrimSpace(input.RequestedBy)
	if user, ok := userctx.CurrentUserFromContext(ctx); ok && user != nil {
		requestedBy = cmp.Or(strings.TrimSpace(kit.FromPtr(user.DisplayName)), strings.TrimSpace(user.Username), requestedBy)
	}
	cancelled, err := h.activityService.CancelActivity(ctx, input.EnvironmentID, input.ActivityID, requestedBy)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if input.EnvironmentID != "0" {
				path := fmt.Sprintf("/api/environments/0/activities/%s/cancel", url.PathEscape(input.ActivityID))
				if requestedBy != "" {
					path += "?requestedBy=" + url.QueryEscape(requestedBy)
				}
				out, proxyErr := h.environment.ProxyJSONRequest.JSON[types.ApiResponse[types.Activity]](ctx, input.EnvironmentID, http.MethodPost, path, nil)
				if proxyErr != nil {
					return nil, proxyErr
				}
				h.applyActivitySourceLabel(ctx, input.EnvironmentID, &out.Data)
				return &handlerutil.Out[types.Activity]{Body: *out}, nil
			}
			return nil, huma.Error404NotFound("activity not found")
		case errors.Is(err, ErrActivityNotCancelable):
			return nil, huma.Error409Conflict("activity is not running")
		default:
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}
	h.applyActivitySourceLabel(ctx, input.EnvironmentID, cancelled)

	return &handlerutil.Out[types.Activity]{
		Body: types.ApiResponse[types.Activity]{
			Success: true,
			Data:    *cancelled,
		},
	}, nil
}

func (h *ActivityHandler) RunLocalStreamProducer(ctx context.Context, limit int, events chan<- types.ActivityStreamEvent) {
	sendSnapshot := func() bool {
		activities, _, err := h.activityService.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{
			Limit:     resolveActivityStreamLimit(limit),
			SkipCount: true,
		})
		if err != nil {
			if ctx.Err() == nil {
				agg.Send(ctx, events, types.ActivityStreamEvent{
					Type:          "error",
					EnvironmentID: "0",
					Error:         err.Error(),
					Timestamp:     time.Now(),
				})
			}
			return false
		}
		h.applyActivitySourceLabels(ctx, "0", activities)
		return agg.Send(ctx, events, types.ActivityStreamEvent{
			Type:          "snapshot",
			EnvironmentID: "0",
			Activities:    activities,
			Timestamp:     time.Now(),
		})
	}

	eventCh, missedEvents, unsubscribe := h.activityService.pubsub.Subscribe("0")
	defer unsubscribe()

	snapshotOK := sendSnapshot()

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
			if !h.canReadActivityStreamEvent(ctx, event) {
				continue
			}
			event.EnvironmentID = "0"
			if event.Activity != nil {
				h.applyActivitySourceLabel(ctx, "0", event.Activity)
			}
			if len(event.Activities) > 0 {
				h.applyActivitySourceLabels(ctx, "0", event.Activities)
			}
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
func (h *ActivityHandler) RunRemoteStreamPollers(ctx context.Context, ps *authz.PermissionSet, limit int, events chan<- types.ActivityStreamEvent) {
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]environment.Environment, error) {
			environments, err := h.environment.ListActiveRemoteEnvironments(ctx)
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
		activityStreamEnvironmentVersion,
		activityStreamEnvReconcileInterval,
		"activity stream",
		func(pollCtx context.Context, environment environment.Environment) {
			// One shared poller per environment (and snapshot limit) serves
			// every connected client; this subscriber only forwards its
			// events onto this client's stream.
			key := activityStreamEnvironmentVersion(environment) + ":" + strconv.Itoa(resolveActivityStreamLimit(limit))
			h.remoteStreamHub.Subscribe(pollCtx, key,
				func(runCtx context.Context, publish func(types.ActivityStreamEvent)) {
					h.runRemoteActivityStreamPoller(runCtx, environment, limit, publish)
				},
				func(event types.ActivityStreamEvent) bool {
					return agg.Send(pollCtx, events, event)
				})
		})
}

func activityStreamEnvironmentVersion(localEnvironment environment.Environment) string {
	if localEnvironment.UpdatedAt == nil {
		return localEnvironment.ID
	}
	return localEnvironment.ID + ":" + localEnvironment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func (h *ActivityHandler) runRemoteActivityStreamPoller(ctx context.Context, localEnvironment environment.Environment, limit int, publish func(types.ActivityStreamEvent)) {
	environmentID := localEnvironment.ID
	lastFingerprint := ""

	poll := func() {
		currentEnvironment := localEnvironment
		if h.environment.GetActiveRemoteEnvironment != nil {
			var ok bool
			currentEnvironment, ok = h.environment.GetActiveRemoteEnvironment(environmentID).Get()
			if !ok {
				return
			}
		}

		output, err := h.listRemoteActivities(ctx, &ListActivitiesInput{
			EnvironmentID: environmentID,
			Limit:         resolveActivityStreamLimit(limit),
			Order:         "desc",
		}, func(path string) (*types.Paginated[types.Activity], error) {
			pollCtx, cancelPoll := context.WithTimeout(ctx, activityStreamRemotePollTimeout)
			defer cancelPoll()
			var out types.Paginated[types.Activity]
			if err := h.environment.ProxyJSONRequestForEnvironment(pollCtx, currentEnvironment, http.MethodGet, path, nil, &out); err != nil {
				return nil, handlerutil.TranslateRemoteProxyError(err)
			}
			return &out, nil
		})
		if err != nil && ctx.Err() != nil {
			return
		}
		activities := kit.FromPtr(output).Body.Data
		sourceID := cmp.Or(currentEnvironment.ID, environment.LocalEnvironmentID)
		sourceName := environment.DisplayName(sourceID, currentEnvironment.Name)
		for i := range activities {
			applyActivitySource(&activities[i], sourceID, sourceName)
		}
		errMessage := func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}()
		fingerprint := strconv.FormatUint(kit.Fingerprint(activities), 16) + "|" + errMessage
		if fingerprint == lastFingerprint {
			return
		}
		lastFingerprint = fingerprint
		publish(types.ActivityStreamEvent{
			Type:          kit.Ternary(err == nil, "snapshot", "error"),
			EnvironmentID: environmentID,
			Activities:    activities,
			Error:         errMessage,
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

func (h *ActivityHandler) applyActivitySourceLabels(ctx context.Context, environmentID string, activities []types.Activity) {
	sourceID := cmp.Or(environmentID, environment.LocalEnvironmentID)
	sourceName := environment.DisplayName(sourceID, "")
	if h.environment.ResolveEnvironmentName != nil {
		sourceName = h.environment.ResolveEnvironmentName(ctx, sourceID)
	}
	for i := range activities {
		applyActivitySource(&activities[i], sourceID, sourceName)
	}
}

func (h *ActivityHandler) applyActivitySourceLabel(ctx context.Context, environmentID string, item *types.Activity) {
	items := []types.Activity{*item}
	h.applyActivitySourceLabels(ctx, environmentID, items)
	*item = items[0]
}

func applyActivitySource(item *types.Activity, sourceID, sourceName string) {
	item.SourceEnvironmentID = sourceID
	item.SourceEnvironmentName = sourceName
	item.EnvironmentID = sourceID
	if item.Type == types.ActivityTypeJobRun && item.Metadata != nil {
		item.Metadata = maps.Clone(item.Metadata)
		item.Metadata["environmentId"] = sourceID
	}
}

func resolveActivityStreamLimit(limit int) int {
	return min(kit.Ternary(limit <= 0, 50, limit), 100)
}

// activityListParams maps list input onto pagination params and its filters.
func activityListParams(input *ListActivitiesInput) pagination.QueryParams {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	maps.Copy(params.Filters, map[string]string{
		"status":       input.Status,
		"type":         input.Type,
		"resourceType": input.ResourceType,
	})
	return params
}

func (h *ActivityHandler) listRemoteActivities(
	ctx context.Context,
	input *ListActivitiesInput,
	fetch func(string) (*types.Paginated[types.Activity], error),
) (*handlerutil.Page[types.Activity], error) {
	params := normalizeRemoteActivityParams(activityListParams(input))
	// The agent only ships the newest window the merged page can use, not its
	// whole history; the stream poller repeats this every few seconds.
	window := kit.Ternary(params.Limit == -1, -1, params.Start+params.Limit)
	remoteActivities, remoteTotal, remoteErr := collectActivityWindow(window, func(start, limit int) ([]types.Activity, int64, error) {
		query := url.Values{
			"start": {strconv.Itoa(start)},
			"limit": {strconv.Itoa(limit)},
		}
		for key, value := range map[string]string{
			"search":       input.Search,
			"sort":         input.Sort,
			"order":        input.Order,
			"status":       input.Status,
			"type":         input.Type,
			"resourceType": input.ResourceType,
		} {
			if value != "" {
				query.Set(key, value)
			}
		}
		remote, err := fetch("/api/environments/0/activities?" + query.Encode())
		if err != nil {
			return nil, 0, err
		}
		return remote.Data, max(remote.Pagination.TotalItems, 0), nil
	})
	// The proxy may have exhausted its timeout; manager summaries still need
	// to be read so an offline environment's queued work remains visible.
	activities, page, err := h.activityService.ListRemoteActivities(ctx, input.EnvironmentID, remoteActivities, remoteTotal, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.applyActivitySourceLabels(ctx, input.EnvironmentID, activities)
	return &handlerutil.Page[types.Activity]{Body: types.Paginated[types.Activity]{
		Success:    remoteErr == nil,
		Data:       activities,
		Pagination: handlerutil.PaginationResponse(page),
	}}, remoteErr
}
