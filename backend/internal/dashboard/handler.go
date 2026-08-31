package dashboard

import (
	"context"
	stderrors "errors"
	"net/http"
	"time"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"go.getarcane.app/streams/agg"
)

type DashboardHandler struct {
	dashboardService   *DashboardService
	environmentService *environment.EnvironmentService

	// remoteStreamHub shares one poller per remote environment across every
	// connected stream client instead of polling per client × environment.
	remoteStreamHub *agg.Hub[dashboardtypes.StreamEvent]
}

type GetDashboardInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	DebugAllGood  bool   `query:"debugAllGood" default:"false" doc:"Debug mode: force an empty action item list"`
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
		remoteStreamHub:    agg.NewHub[dashboardtypes.StreamEvent](),
	}
}

func RegisterDashboard(api huma.API, dashboardService *DashboardService, environmentService *environment.EnvironmentService) {
	h := NewHandler(dashboardService, environmentService)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-dashboard",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/dashboard",
		Summary:     "Get dashboard snapshot",
		Description: "Returns the dashboard first-paint snapshot in a single response",
		Tags:        []string{"Dashboard"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDashboardRead, h.GetDashboard)
}

func (h *DashboardHandler) GetDashboard(ctx context.Context, input *GetDashboardInput) (*handlerutil.Out[dashboardtypes.Snapshot], error) {
	// EnvironmentID is consumed by env proxy/auth middleware for routing/validation.
	_ = input.EnvironmentID

	snapshot, err := h.dashboardService.GetSnapshot(ctx, DashboardActionItemsOptions{
		DebugAllGood: input.DebugAllGood,
	}, true)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if snapshot == nil {
		return nil, huma.Error500InternalServerError("dashboard snapshot not available")
	}

	return &handlerutil.Out[dashboardtypes.Snapshot]{
		Body: base.ApiResponse[dashboardtypes.Snapshot]{
			Success: true,
			Data:    *snapshot,
		},
	}, nil
}

// trimDashboardStreamSnapshotInternal drops the first-page container/image
// tables: the all-environments dashboard only reads the aggregate counters,
// and re-sending table rows for every environment on every poll would bloat
// the stream. Only remote snapshots (decoded fresh per poll) pass through
// here; the local producer gets a snapshot built without tables instead.
func trimDashboardStreamSnapshotInternal(snapshot *dashboardtypes.Snapshot) *dashboardtypes.Snapshot {
	if snapshot == nil {
		return nil
	}
	snapshot.Containers.Data = nil
	snapshot.Images.Data = nil
	return snapshot
}

func (h *DashboardHandler) RunLocalStreamProducer(ctx context.Context, debugAllGood bool, events chan<- dashboardtypes.StreamEvent) {
	lastError := ""

	poll := func() {
		snapshot, err := h.dashboardService.GetSnapshot(ctx, DashboardActionItemsOptions{
			DebugAllGood: debugAllGood,
		}, false)
		if err == nil && snapshot == nil {
			err = common.Classify(common.ErrUnavailable, errors.New("dashboard snapshot not available"))
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A failing snapshot must not end the stream; surface the error
			// once per distinct message and keep polling.
			if msg := err.Error(); msg != lastError {
				lastError = msg
				agg.Send(ctx, events, dashboardtypes.StreamEvent{
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
		agg.Send(ctx, events, dashboardtypes.StreamEvent{
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
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

// RunRemoteStreamPollers keeps one poller goroutine per
// enabled remote environment, re-listing periodically so environments added
// or removed while the stream is open are picked up without a reconnect.
func (h *DashboardHandler) RunRemoteStreamPollers(ctx context.Context, ps *authz.PermissionSet, debugAllGood bool, events chan<- dashboardtypes.StreamEvent) {
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]environment.Environment, error) {
			environments, err := h.environmentService.ListRemoteEnvironments(ctx)
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
		dashboardStreamEnvironmentVersionInternal,
		dashboardStreamEnvReconcileInterval,
		"dashboard stream",
		func(pollCtx context.Context, environment environment.Environment) {
			// One shared poller per environment (and debug variant) serves
			// every connected client; this subscriber only forwards its
			// events onto this client's stream.
			key := dashboardStreamEnvironmentVersionInternal(environment)
			if debugAllGood {
				key += ":debugAllGood"
			}
			h.remoteStreamHub.Subscribe(pollCtx, key,
				func(runCtx context.Context, publish func(dashboardtypes.StreamEvent)) {
					h.runRemoteDashboardStreamPollerInternal(runCtx, environment, debugAllGood, publish)
				},
				func(event dashboardtypes.StreamEvent) bool {
					return agg.Send(pollCtx, events, event)
				})
		})
}

func dashboardStreamEnvironmentVersionInternal(environment environment.Environment) string {
	if environment.UpdatedAt == nil {
		return environment.ID
	}
	return environment.ID + ":" + environment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func (h *DashboardHandler) runRemoteDashboardStreamPollerInternal(ctx context.Context, environment environment.Environment, debugAllGood bool, publish func(dashboardtypes.StreamEvent)) {
	environmentID := environment.ID
	// Tell the client this environment is covered before the first poll
	// completes so it can hold skeletons instead of assuming no data exists.
	publish(dashboardtypes.StreamEvent{
		Type:          "pending",
		EnvironmentID: environmentID,
		Timestamp:     time.Now(),
	})

	lastError := ""

	poll := func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, dashboardStreamRemotePollTimeout)
		defer cancelPoll()

		currentEnvironment := environment
		if h.environmentService != nil {
			var ok bool
			currentEnvironment, ok = h.environmentService.GetActiveRemoteEnvironmentSnapshot(environmentID).Get()
			if !ok {
				return
			}
		}

		snapshot, err := h.fetchRemoteDashboardSnapshotInternal(pollCtx, currentEnvironment, debugAllGood)
		if err != nil && isDashboardEndpointMissingInternal(err) {
			// The agent runs a version without (or with an incompatible)
			// aggregate dashboard endpoint; the underlying data is still
			// there, so compose the snapshot from the granular endpoints.
			snapshot, err = h.fetchLegacyDashboardSnapshotInternal(pollCtx, currentEnvironment)
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A failing environment must not end the stream; surface the error
			// once per distinct message and keep polling.
			message, code := classifyDashboardStreamErrorInternal(err)
			if message != lastError {
				lastError = message
				publish(dashboardtypes.StreamEvent{
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
		publish(dashboardtypes.StreamEvent{
			Type:          "snapshot",
			EnvironmentID: environmentID,
			Snapshot:      trimDashboardStreamSnapshotInternal(snapshot),
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

// fetchRemoteDashboardSnapshotInternal proxies the per-environment dashboard
// endpoint directly through the environment service so the raw remenv error
// survives for classification (proxyRemoteJSONInternal would translate it
// into a huma error first).
func (h *DashboardHandler) fetchRemoteDashboardSnapshotInternal(ctx context.Context, environment environment.Environment, debugAllGood bool) (*dashboardtypes.Snapshot, error) {
	path := "/api/environments/0/dashboard"
	if debugAllGood {
		path += "?debugAllGood=true"
	}

	var out base.ApiResponse[dashboardtypes.Snapshot]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, common.Classify(common.ErrUnavailable, errors.New("dashboard snapshot not available"))
	}
	return &out.Data, nil
}

// fetchLegacyDashboardSnapshotInternal composes a dashboard snapshot from the
// granular endpoints (container counts, image usage counts, app version) that
// agents have exposed for far longer than the aggregate dashboard endpoint.
// Each piece is fetched independently so a partially compatible agent still
// yields partial data; only when every piece fails is an error returned.
func (h *DashboardHandler) fetchLegacyDashboardSnapshotInternal(ctx context.Context, environment environment.Environment) (*dashboardtypes.Snapshot, error) {
	snapshot := &dashboardtypes.Snapshot{
		ActionItems: dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}},
	}
	var errs []error
	attempted := 0

	attempted++
	var containerCounts base.ApiResponse[containertypes.StatusCounts]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, "/api/environments/0/containers/counts", nil, &containerCounts); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.Containers.Counts = containerCounts.Data
		if stopped := containerCounts.Data.StoppedContainers; stopped > 0 {
			snapshot.ActionItems.Items = append(snapshot.ActionItems.Items, dashboardtypes.ActionItem{
				Kind:     dashboardtypes.ActionItemKindStoppedContainers,
				Count:    stopped,
				Severity: dashboardtypes.ActionItemSeverityWarning,
			})
		}
	}

	attempted++
	var imageCounts base.ApiResponse[imagetypes.UsageCounts]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, "/api/environments/0/images/counts", nil, &imageCounts); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.ImageUsageCounts = imageCounts.Data
	}

	attempted++
	var volumeCounts base.ApiResponse[volumetypes.UsageCounts]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, "/api/environments/0/volumes/counts", nil, &volumeCounts); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.VolumeUsageCounts = &volumeCounts.Data
	}

	attempted++
	var versionInfo versiontypes.Info
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, "/api/app-version", nil, &versionInfo); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.VersionInfo = &versionInfo
	}

	if len(errs) == attempted {
		return nil, stderrors.Join(errs...)
	}
	return snapshot, nil
}

// isDashboardEndpointMissingInternal reports whether the aggregate dashboard
// endpoint is absent (404 on older agents) or speaks an incompatible payload
// shape (decode failure) — the cases the legacy composition can recover from.
func isDashboardEndpointMissingInternal(err error) bool {
	if statusErr, ok := stderrors.AsType[*remenv.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return true
	}
	_, ok := stderrors.AsType[*remenv.DecodeError](err)
	return ok
}

// classifyDashboardStreamErrorInternal maps remote fetch failures to a
// user-facing message and a stable error code. A 404 means the agent predates
// the dashboard endpoint; a decode failure means its payload shape differs —
// both indicate a version mismatch between manager and agent.
func classifyDashboardStreamErrorInternal(err error) (string, string) {
	if isDashboardEndpointMissingInternal(err) {
		return "Agent does not provide the dashboard endpoint — the agent is likely running an older Arcane version and should be upgraded", dashboardtypes.StreamErrorCodeAgentIncompatible
	}
	if transportErr, ok := stderrors.AsType[*remenv.TransportError](err); ok {
		return transportErr.Error(), dashboardtypes.StreamErrorCodeUnreachable
	}
	return err.Error(), ""
}
