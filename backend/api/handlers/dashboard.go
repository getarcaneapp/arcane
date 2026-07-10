package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	"go.getarcane.app/streams/agg"
)

type DashboardHandler struct {
	dashboardService   *services.DashboardService
	environmentService *services.EnvironmentService
}

type GetDashboardInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	DebugAllGood  bool   `query:"debugAllGood" default:"false" doc:"Debug mode: force an empty action item list"`
}

type GetDashboardOutput struct {
	Body base.ApiResponse[dashboardtypes.Snapshot]
}

type StreamAllDashboardsInput struct {
	DebugAllGood bool `query:"debugAllGood" default:"false" doc:"Debug mode: force an empty action item list"`
}

const (
	dashboardStreamHeartbeatInterval    = 15 * time.Second
	dashboardStreamLocalPollInterval    = 15 * time.Second
	dashboardStreamRemotePollInterval   = 15 * time.Second
	dashboardStreamEnvReconcileInterval = 30 * time.Second
	dashboardStreamRemotePollTimeout    = 15 * time.Second
	dashboardStreamEventBuffer          = 64
)

func RegisterDashboard(api huma.API, dashboardService *services.DashboardService, environmentService *services.EnvironmentService) {
	h := &DashboardHandler{
		dashboardService:   dashboardService,
		environmentService: environmentService,
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-dashboard",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/dashboard",
		Summary:     "Get dashboard snapshot",
		Description: "Returns the dashboard first-paint snapshot in a single response",
		Tags:        []string{"Dashboard"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermDashboardRead, h.GetDashboard)

	huma.Register(api, huma.Operation{
		OperationID: "stream-all-dashboards",
		Method:      http.MethodGet,
		Path:        "/dashboard/stream",
		Summary:     "Stream dashboard snapshots across all environments",
		Description: "Stream dashboard snapshot updates for the local environment and all enabled remote environments as JSON lines",
		Tags:        []string{"Dashboard"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequireAnyEnvironmentPermission(api, authz.PermDashboardRead),
	}, h.StreamAllDashboards)
}

func (h *DashboardHandler) GetDashboard(ctx context.Context, input *GetDashboardInput) (*GetDashboardOutput, error) {
	if h.dashboardService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// EnvironmentID is consumed by env proxy/auth middleware for routing/validation.
	_ = input.EnvironmentID

	snapshot, err := h.dashboardService.GetSnapshot(ctx, services.DashboardActionItemsOptions{
		DebugAllGood: input.DebugAllGood,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if snapshot == nil {
		return nil, huma.Error500InternalServerError("dashboard snapshot not available")
	}

	return &GetDashboardOutput{
		Body: base.ApiResponse[dashboardtypes.Snapshot]{
			Success: true,
			Data:    *snapshot,
		},
	}, nil
}

func (h *DashboardHandler) StreamAllDashboards(ctx context.Context, input *StreamAllDashboardsInput) (*huma.StreamResponse, error) {
	if h.dashboardService == nil || h.environmentService == nil {
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

			ps, _ := humamw.PermissionsFromContext(humaCtx.Context())
			h.streamAllDashboardsInternal(humaCtx.Context(), ps, input.DebugAllGood, encoder, flush)
		},
	}, nil
}

// streamAllDashboardsInternal multiplexes dashboard snapshots for the local
// environment and every enabled remote environment over a single response so
// the browser needs one connection regardless of environment count.
func (h *DashboardHandler) streamAllDashboardsInternal(ctx context.Context, ps *authz.PermissionSet, debugAllGood bool, encoder *json.Encoder, flush func()) {
	_ = httpx.RunAuthorizedAggregateStream(ctx, ps, authz.PermDashboardRead, agg.Config[dashboardtypes.StreamEvent]{
		Encoder:           encoder,
		Flush:             flush,
		Buffer:            dashboardStreamEventBuffer,
		HeartbeatInterval: dashboardStreamHeartbeatInterval,
		MakeHeartbeat: func() dashboardtypes.StreamEvent {
			return dashboardtypes.StreamEvent{Type: "heartbeat", Timestamp: time.Now()}
		},
	},
		func(ctx context.Context, events chan<- dashboardtypes.StreamEvent) {
			h.runLocalDashboardStreamProducerInternal(ctx, debugAllGood, events)
		},
		func(ctx context.Context, events chan<- dashboardtypes.StreamEvent) {
			h.runRemoteDashboardStreamPollersInternal(ctx, ps, debugAllGood, events)
		})
}

// trimDashboardStreamSnapshotInternal drops the first-page container/image
// tables: the all-environments dashboard only reads the aggregate counters,
// and re-sending table rows for every environment on every poll would bloat
// the stream.
func trimDashboardStreamSnapshotInternal(snapshot *dashboardtypes.Snapshot) *dashboardtypes.Snapshot {
	if snapshot == nil {
		return nil
	}
	snapshot.Containers.Data = nil
	snapshot.Images.Data = nil
	return snapshot
}

func (h *DashboardHandler) runLocalDashboardStreamProducerInternal(ctx context.Context, debugAllGood bool, events chan<- dashboardtypes.StreamEvent) {
	lastError := ""

	poll := func() {
		snapshot, err := h.dashboardService.GetSnapshot(ctx, services.DashboardActionItemsOptions{
			DebugAllGood: debugAllGood,
		})
		if err == nil && snapshot == nil {
			err = &common.DashboardSnapshotUnavailableError{}
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
		agg.Send(ctx, events, dashboardtypes.StreamEvent{
			Type:          "snapshot",
			EnvironmentID: "0",
			Snapshot:      trimDashboardStreamSnapshotInternal(snapshot),
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

// runRemoteDashboardStreamPollersInternal keeps one poller goroutine per
// enabled remote environment, re-listing periodically so environments added
// or removed while the stream is open are picked up without a reconnect.
func (h *DashboardHandler) runRemoteDashboardStreamPollersInternal(ctx context.Context, ps *authz.PermissionSet, debugAllGood bool, events chan<- dashboardtypes.StreamEvent) {
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]models.Environment, error) {
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
		func(environment models.Environment) string {
			return environment.ID
		},
		dashboardStreamEnvironmentVersionInternal,
		dashboardStreamEnvReconcileInterval,
		"dashboard stream",
		func(pollCtx context.Context, environment models.Environment) {
			h.runRemoteDashboardStreamPollerInternal(pollCtx, environment, debugAllGood, events)
		})
}

func dashboardStreamEnvironmentVersionInternal(environment models.Environment) string {
	if environment.UpdatedAt == nil {
		return environment.ID
	}
	return environment.ID + ":" + environment.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func (h *DashboardHandler) runRemoteDashboardStreamPollerInternal(ctx context.Context, environment models.Environment, debugAllGood bool, events chan<- dashboardtypes.StreamEvent) {
	environmentID := environment.ID
	// Tell the client this environment is covered before the first poll
	// completes so it can hold skeletons instead of assuming no data exists.
	if !agg.Send(ctx, events, dashboardtypes.StreamEvent{
		Type:          "pending",
		EnvironmentID: environmentID,
		Timestamp:     time.Now(),
	}) {
		return
	}

	lastError := ""

	poll := func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, dashboardStreamRemotePollTimeout)
		defer cancelPoll()

		currentEnvironment := environment
		if h.environmentService != nil {
			var ok bool
			currentEnvironment, ok = h.environmentService.GetActiveRemoteEnvironmentSnapshot(environmentID)
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
				agg.Send(ctx, events, dashboardtypes.StreamEvent{
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
		agg.Send(ctx, events, dashboardtypes.StreamEvent{
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
func (h *DashboardHandler) fetchRemoteDashboardSnapshotInternal(ctx context.Context, environment models.Environment, debugAllGood bool) (*dashboardtypes.Snapshot, error) {
	path := "/api/environments/0/dashboard"
	if debugAllGood {
		path += "?debugAllGood=true"
	}

	var out base.ApiResponse[dashboardtypes.Snapshot]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, &common.DashboardSnapshotUnavailableError{}
	}
	return &out.Data, nil
}

// fetchLegacyDashboardSnapshotInternal composes a dashboard snapshot from the
// granular endpoints (container counts, image usage counts, app version) that
// agents have exposed for far longer than the aggregate dashboard endpoint.
// Each piece is fetched independently so a partially compatible agent still
// yields partial data; only when every piece fails is an error returned.
func (h *DashboardHandler) fetchLegacyDashboardSnapshotInternal(ctx context.Context, environment models.Environment) (*dashboardtypes.Snapshot, error) {
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
	var versionInfo versiontypes.Info
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, "/api/app-version", nil, &versionInfo); err != nil {
		errs = append(errs, err)
	} else {
		snapshot.VersionInfo = &versionInfo
	}

	if len(errs) == attempted {
		return nil, errors.Join(errs...)
	}
	return snapshot, nil
}

// isDashboardEndpointMissingInternal reports whether the aggregate dashboard
// endpoint is absent (404 on older agents) or speaks an incompatible payload
// shape (decode failure) — the cases the legacy composition can recover from.
func isDashboardEndpointMissingInternal(err error) bool {
	if statusErr, ok := errors.AsType[*remenv.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return true
	}
	_, ok := errors.AsType[*remenv.DecodeError](err)
	return ok
}

// classifyDashboardStreamErrorInternal maps remote fetch failures to a
// user-facing message and a stable error code. A 404 means the agent predates
// the dashboard endpoint; a decode failure means its payload shape differs —
// both indicate a version mismatch between manager and agent.
func classifyDashboardStreamErrorInternal(err error) (string, string) {
	if isDashboardEndpointMissingInternal(err) {
		return (&common.AgentDashboardUnsupportedError{}).Error(), dashboardtypes.StreamErrorCodeAgentIncompatible
	}
	if transportErr, ok := errors.AsType[*remenv.TransportError](err); ok {
		return transportErr.Error(), dashboardtypes.StreamErrorCodeUnreachable
	}
	return err.Error(), ""
}
