package dashboard

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/streams/agg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type DashboardHandler struct {
	dashboardService   *DashboardService
	environmentService *environment.EnvironmentService

	// remoteStreamHub shares one poller per remote environment across every
	// connected stream client instead of polling per client × environment.
	remoteStreamHub *agg.Hub[types.DashboardStreamEvent]
	localStreamHub  *agg.Hub[types.DashboardStreamEvent]
}

type GetDashboardInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	DebugAllGood  bool   `query:"debugAllGood" default:"false" doc:"Debug mode: force an empty action item list"`
	IncludeTables bool   `query:"includeTables" default:"true" doc:"Include the first-page container and image tables"`
}

const (
	dashboardStreamHeartbeatInterval    = 15 * time.Second
	dashboardStreamLocalPollInterval    = 15 * time.Second
	dashboardStreamRemotePollInterval   = 15 * time.Second
	dashboardStreamEnvReconcileInterval = 30 * time.Second
	dashboardStreamRemotePollTimeout    = 15 * time.Second
	dashboardStreamEventBuffer          = 64
)

// NewHandler builds the dashboard HTTP handler. Cross-domain handlers (the
// multiplexed client stream) compose it rather than reaching into its fields.
func NewHandler(dashboardService *DashboardService, environmentService *environment.EnvironmentService) *DashboardHandler {
	return &DashboardHandler{
		dashboardService:   dashboardService,
		environmentService: environmentService,
		remoteStreamHub:    agg.NewHub[types.DashboardStreamEvent](),
		localStreamHub:     agg.NewHub[types.DashboardStreamEvent](),
	}
}

func (h *DashboardHandler) GetDashboard(ctx context.Context, input *GetDashboardInput) (*handlerutil.Out[types.DashboardSnapshot], error) {
	// EnvironmentID is consumed by env proxy/auth middleware for routing/validation.
	_ = input.EnvironmentID

	snapshot, err := h.dashboardService.GetSnapshot(ctx, DashboardActionItemsOptions{
		DebugAllGood: input.DebugAllGood,
	}, input.IncludeTables)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if snapshot == nil {
		return nil, huma.Error500InternalServerError("dashboard snapshot not available")
	}

	return &handlerutil.Out[types.DashboardSnapshot]{
		Body: types.ApiResponse[types.DashboardSnapshot]{
			Success: true,
			Data:    *snapshot,
		},
	}, nil
}

// trimDashboardStreamSnapshot drops the first-page container/image
// tables: the all-environments dashboard only reads the aggregate counters,
// and re-sending table rows for every environment on every poll would bloat
// the stream. Only remote snapshots (decoded fresh per poll) pass through
// here; the local producer gets a snapshot built without tables instead.
func trimDashboardStreamSnapshot(snapshot *types.DashboardSnapshot) *types.DashboardSnapshot {
	if snapshot == nil {
		return nil
	}
	snapshot.Containers.Data = nil
	snapshot.Images.Data = nil
	return snapshot
}

func (h *DashboardHandler) RunLocalStreamProducer(ctx context.Context, debugAllGood bool, events chan<- types.DashboardStreamEvent) {
	h.localStreamHub.Subscribe(ctx, kit.Ternary(debugAllGood, "local:debugAllGood", "local"),
		func(runCtx context.Context, publish func(types.DashboardStreamEvent)) {
			lastError := ""

			poll := func() {
				snapshot, err := h.dashboardService.GetSnapshot(runCtx, DashboardActionItemsOptions{
					DebugAllGood: debugAllGood,
				}, false)
				if err == nil && snapshot == nil {
					err = common.Classify(common.ErrUnavailable, errors.New("dashboard snapshot not available"))
				}
				if err != nil {
					if runCtx.Err() != nil {
						return
					}
					// A failing snapshot must not end the stream; surface the error
					// once per distinct message and keep polling.
					if msg := err.Error(); msg != lastError {
						lastError = msg
						publish(types.DashboardStreamEvent{
							Type:          "error",
							EnvironmentID: "0",
							Error:         msg,
							Timestamp:     time.Now(),
						})
					}
					return
				}
				lastError = ""
				// Already built without tables; shared with other subscribers, so it
				// must not be trimmed (mutated) here.
				publish(types.DashboardStreamEvent{
					Type:          "snapshot",
					EnvironmentID: "0",
					Snapshot:      snapshot,
					Timestamp:     time.Now(),
				})
			}

			poll()

			ticker := time.NewTicker(dashboardStreamLocalPollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					poll()
				}
			}
		},
		func(event types.DashboardStreamEvent) bool {
			return agg.Send(ctx, events, event)
		})
}

// RunRemoteStreamPollers keeps one poller goroutine per
// enabled remote environment, re-listing periodically so environments added
// or removed while the stream is open are picked up without a reconnect.
func (h *DashboardHandler) RunRemoteStreamPollers(ctx context.Context, ps *authz.PermissionSet, debugAllGood bool, events chan<- types.DashboardStreamEvent) {
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]environment.Environment, error) {
			environments, err := h.environmentService.ListActiveRemoteEnvironments(ctx)
			if err != nil {
				return nil, err
			}
			allowed := environments[:0]
			for _, environment := range environments {
				if ps.Allows(authz.PermDashboardRead, environment.ID) {
					allowed = append(allowed, environment)
				}
			}
			return allowed, nil
		},
		func(environment environment.Environment) string {
			return environment.ID
		},
		dashboardStreamEnvironmentVersion,
		dashboardStreamEnvReconcileInterval,
		"dashboard stream",
		func(pollCtx context.Context, environment environment.Environment) {
			// One shared poller per environment (and debug variant) serves
			// every connected client; this subscriber only forwards its
			// events onto this client's stream.
			key := dashboardStreamEnvironmentVersion(environment)
			if debugAllGood {
				key += ":debugAllGood"
			}
			h.remoteStreamHub.Subscribe(pollCtx, key,
				func(runCtx context.Context, publish func(types.DashboardStreamEvent)) {
					h.runRemoteDashboardStreamPoller(runCtx, environment, debugAllGood, publish)
				},
				func(event types.DashboardStreamEvent) bool {
					return agg.Send(pollCtx, events, event)
				})
		})
}

func dashboardStreamEnvironmentVersion(localEnvironment environment.Environment) string {
	if localEnvironment.UpdatedAt == nil {
		return localEnvironment.ID
	}
	return localEnvironment.ID + ":" + localEnvironment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func (h *DashboardHandler) runRemoteDashboardStreamPoller(ctx context.Context, localEnvironment environment.Environment, debugAllGood bool, publish func(types.DashboardStreamEvent)) {
	environmentID := localEnvironment.ID
	// Tell the client this environment is covered before the first poll
	// completes so it can hold skeletons instead of assuming no data exists.
	publish(types.DashboardStreamEvent{
		Type:          "pending",
		EnvironmentID: environmentID,
		Timestamp:     time.Now(),
	})

	lastError := ""

	poll := func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, dashboardStreamRemotePollTimeout)
		defer cancelPoll()

		currentEnvironment := localEnvironment
		if h.environmentService != nil {
			var ok bool
			currentEnvironment, ok = h.environmentService.GetActiveRemoteEnvironmentSnapshot(environmentID).Get()
			if !ok {
				return
			}
		}

		snapshot, err := h.fetchRemoteDashboardSnapshot(pollCtx, currentEnvironment, debugAllGood)
		if err != nil && isDashboardEndpointMissing(err) {
			// The agent runs a version without (or with an incompatible)
			// aggregate dashboard endpoint; the underlying data is still
			// there, so compose the snapshot from the granular endpoints.
			snapshot, err = h.fetchLegacyDashboardSnapshot(pollCtx, currentEnvironment)
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A failing environment must not end the stream; surface the error
			// once per distinct message and keep polling.
			message, code := classifyDashboardStreamError(err)
			if message != lastError {
				lastError = message
				publish(types.DashboardStreamEvent{
					Type:          "error",
					EnvironmentID: environmentID,
					Error:         message,
					ErrorCode:     code,
					Timestamp:     time.Now(),
				})
			}
			return
		}
		lastError = ""
		// A successful snapshot proves the manager reaches this direct agent; edge liveness comes from the tunnel.
		if !currentEnvironment.IsEdge {
			// Legacy snapshots may succeed after the fetch deadline.
			heartbeatCtx, cancelHeartbeat := context.WithTimeout(ctx, 5*time.Second)
			defer cancelHeartbeat()
			if heartbeatErr := h.environmentService.UpdateEnvironmentHeartbeat(heartbeatCtx, environmentID); heartbeatErr != nil {
				slog.WarnContext(ctx, "Failed to update environment heartbeat", "environmentId", environmentID, "error", heartbeatErr)
			}
		}
		publish(types.DashboardStreamEvent{
			Type:          "snapshot",
			EnvironmentID: environmentID,
			Snapshot:      trimDashboardStreamSnapshot(snapshot),
			Timestamp:     time.Now(),
		})
	}

	poll()

	ticker := time.NewTicker(dashboardStreamRemotePollInterval)
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

// fetchRemoteDashboardSnapshot proxies the per-environment dashboard
// endpoint directly through the environment service so the raw remenv error
// survives for classification (proxyRemoteJSONInternal would translate it
// into a huma error first).
func (h *DashboardHandler) fetchRemoteDashboardSnapshot(ctx context.Context, localEnvironment environment.Environment, debugAllGood bool) (*types.DashboardSnapshot, error) {
	// The all-environments dashboard only reads the aggregate counters, so the
	// agent is asked to leave the container/image tables out of the payload.
	query := url.Values{"includeTables": {"false"}}
	if debugAllGood {
		query.Set("debugAllGood", "true")
	}
	path := "/api/environments/0/dashboard?" + query.Encode()

	var out types.ApiResponse[types.DashboardSnapshot]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, localEnvironment, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, common.Classify(common.ErrUnavailable, errors.New("dashboard snapshot not available"))
	}
	return &out.Data, nil
}

// fetchLegacyDashboardSnapshot composes a dashboard snapshot from the
// granular endpoints (container counts, image usage counts, app version) that
// agents have exposed for far longer than the aggregate dashboard endpoint.
// Each piece is fetched independently so a partially compatible agent still
// yields partial data; only when every piece fails is an error returned.
func (h *DashboardHandler) fetchLegacyDashboardSnapshot(ctx context.Context, localEnvironment environment.Environment) (*types.DashboardSnapshot, error) {
	snapshot := &types.DashboardSnapshot{
		ActionItems: types.DashboardActionItems{Items: []types.DashboardActionItem{}},
	}
	var errs []error
	attempted := 0

	attempted++
	var containerCounts types.ApiResponse[types.ContainerStatusCounts]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, localEnvironment, http.MethodGet, "/api/environments/0/containers/counts", nil, &containerCounts); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.Containers.Counts = containerCounts.Data
		if stopped := containerCounts.Data.StoppedContainers; stopped > 0 {
			snapshot.ActionItems.Items = append(snapshot.ActionItems.Items, types.DashboardActionItem{
				Kind:     types.DashboardActionItemKindStoppedContainers,
				Count:    stopped,
				Severity: types.DashboardActionItemSeverityWarning,
			})
		}
	}

	attempted++
	var imageCounts types.ApiResponse[types.ImageUsageCounts]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, localEnvironment, http.MethodGet, "/api/environments/0/images/counts", nil, &imageCounts); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.ImageUsageCounts = imageCounts.Data
	}

	attempted++
	var volumeCounts types.ApiResponse[types.VolumeUsageCounts]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, localEnvironment, http.MethodGet, "/api/environments/0/volumes/counts", nil, &volumeCounts); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.VolumeUsageCounts = &volumeCounts.Data
	}

	attempted++
	var versionInfo types.VersionInfo
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, localEnvironment, http.MethodGet, "/api/app/version", nil, &versionInfo); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.VersionInfo = &versionInfo
	}

	if len(errs) == attempted {
		return nil, errors.Join(errs...)
	}
	return snapshot, nil
}

// isDashboardEndpointMissing reports whether the aggregate dashboard
// endpoint is absent (404 on older agents) or speaks an incompatible payload
// shape (decode failure) — the cases the legacy composition can recover from.
func isDashboardEndpointMissing(err error) bool {
	if statusErr, ok := errors.AsType[*remenv.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return true
	}
	_, ok := errors.AsType[*remenv.DecodeError](err)
	return ok
}

// classifyDashboardStreamError maps remote fetch failures to a
// user-facing message and a stable error code. A 404 means the agent predates
// the dashboard endpoint; a decode failure means its payload shape differs —
// both indicate a version mismatch between manager and agent.
func classifyDashboardStreamError(err error) (string, string) {
	if isDashboardEndpointMissing(err) {
		return "Agent does not provide the dashboard endpoint — the agent is likely running an older Arcane version and should be upgraded", types.DashboardStreamErrorCodeAgentIncompatible
	}
	if transportErr, ok := errors.AsType[*remenv.TransportError](err); ok {
		return transportErr.Error(), types.DashboardStreamErrorCodeUnreachable
	}
	if statusErr, ok := errors.AsType[*remenv.StatusError](err); ok && (statusErr.StatusCode == http.StatusUnauthorized || statusErr.StatusCode == http.StatusForbidden) {
		return "Agent rejected the environment access token — regenerate the API key and update the agent", types.DashboardStreamErrorCodeAuthFailed
	}
	return err.Error(), ""
}
