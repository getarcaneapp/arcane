package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/types/v2/base"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	operationstypes "github.com/getarcaneapp/arcane/types/v2/operations"
	"go.getarcane.app/streams/agg"
)

type GetOperationsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetOperationsOutput struct {
	Body base.ApiResponse[operationstypes.State]
}

type StreamAllOperationsInput struct{}

var operationsReadPermissionsInternal = []string{
	authz.PermImageUpdatesRead,
	authz.PermProjectsList,
	authz.PermProjectsRead,
	authz.PermContainersList,
	authz.PermContainersRead,
	authz.PermVulnsRead,
	authz.PermApiKeysList,
	authz.PermApiKeysRead,
}

func (h *DashboardHandler) registerOperationsInternal(api huma.API) {
	humamw.RegisterWithAnyPermissions(api, huma.Operation{
		OperationID: "get-operations",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/operations",
		Summary:     "Get current operations state",
		Description: "Returns the workload-centric attention summary visible to the caller",
		Tags:        []string{"Operations"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, operationsReadPermissionsInternal, h.GetOperations)

	huma.Register(api, huma.Operation{
		OperationID: "stream-all-operations",
		Method:      http.MethodGet,
		Path:        "/operations/stream",
		Summary:     "Stream live operations state across all environments",
		Description: "Streams permission-filtered operations updates as JSON lines",
		Tags:        []string{"Operations"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequireAnyEnvironmentPermissions(api, operationsReadPermissionsInternal...),
	}, h.StreamAllOperations)
}

// GetOperations returns one environment's current permission-filtered operations state.
func (h *DashboardHandler) GetOperations(ctx context.Context, input *GetOperationsInput) (*GetOperationsOutput, error) {
	if h.dashboardService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	state, err := h.dashboardService.GetOperationsState(ctx, ps, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &GetOperationsOutput{Body: base.ApiResponse[operationstypes.State]{Success: true, Data: *state}}, nil
}

// StreamAllOperations streams live operations updates for the caller's environments.
func (h *DashboardHandler) StreamAllOperations(ctx context.Context, _ *StreamAllOperationsInput) (*huma.StreamResponse, error) {
	if h.dashboardService == nil || h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return &huma.StreamResponse{Body: func(humaCtx huma.Context) { //nolint:contextcheck // stream lifetime follows the HTTP request
		httpx.SetJSONStreamHeaders(humaCtx)
		writer := humaCtx.BodyWriter()
		flush := func() {
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		ps, _ := humamw.PermissionsFromContext(humaCtx.Context())
		h.streamAllOperationsInternal(humaCtx.Context(), ps, writer, flush)
	}}, nil
}

func (h *DashboardHandler) streamAllOperationsInternal(ctx context.Context, ps *authz.PermissionSet, writer io.Writer, flush func()) {
	_ = httpx.RunAuthorizedAggregateStream(ctx, ps, agg.Config[operationstypes.StreamEvent]{
		Writer:            writer,
		Flush:             flush,
		Buffer:            dashboardStreamEventBuffer,
		HeartbeatInterval: dashboardStreamHeartbeatInterval,
		MakeHeartbeat: func() operationstypes.StreamEvent {
			return operationstypes.StreamEvent{Type: "heartbeat", Timestamp: time.Now()}
		},
	}, func(ctx context.Context, events chan<- operationstypes.StreamEvent) {
		h.runLocalOperationsStreamProducerInternal(ctx, ps, events)
	}, func(ctx context.Context, events chan<- operationstypes.StreamEvent) {
		h.runRemoteOperationsStreamPollersInternal(ctx, ps, events)
	}, operationsReadPermissionsInternal...)
}

func (h *DashboardHandler) runLocalOperationsStreamProducerInternal(ctx context.Context, ps *authz.PermissionSet, events chan<- operationstypes.StreamEvent) {
	poll := func() {
		state, err := h.dashboardService.GetOperationsState(ctx, ps, "0")
		if err != nil {
			if ctx.Err() == nil {
				agg.Send(ctx, events, operationstypes.StreamEvent{Type: "error", EnvironmentID: "0", Error: err.Error(), Timestamp: time.Now()})
			}
			return
		}
		agg.Send(ctx, events, operationstypes.StreamEvent{Type: "update", EnvironmentID: "0", State: state, Timestamp: time.Now()})
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

func (h *DashboardHandler) runRemoteOperationsStreamPollersInternal(ctx context.Context, ps *authz.PermissionSet, events chan<- operationstypes.StreamEvent) {
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]models.Environment, error) {
			environments, err := h.environmentService.ListRemoteEnvironments(ctx)
			if err != nil {
				return nil, err
			}
			allowed := environments[:0]
			for _, environment := range environments {
				if operationsAllowedForEnvironmentInternal(ps, environment.ID) {
					allowed = append(allowed, environment)
				}
			}
			return allowed, nil
		},
		func(environment models.Environment) string { return environment.ID },
		dashboardStreamEnvironmentVersionInternal,
		dashboardStreamEnvReconcileInterval,
		"operations stream",
		func(pollCtx context.Context, environment models.Environment) {
			h.runRemoteOperationsStreamPollerInternal(pollCtx, ps, environment, events)
		})
}

func (h *DashboardHandler) runRemoteOperationsStreamPollerInternal(ctx context.Context, ps *authz.PermissionSet, environment models.Environment, events chan<- operationstypes.StreamEvent) {
	environmentID := environment.ID
	if !agg.Send(ctx, events, operationstypes.StreamEvent{Type: "pending", EnvironmentID: environmentID, Timestamp: time.Now()}) {
		return
	}

	poll := func() {
		pollCtx, cancelPoll := context.WithTimeout(ctx, dashboardStreamRemotePollTimeout)
		defer cancelPoll()

		currentEnvironment, ok := h.environmentService.GetActiveRemoteEnvironmentSnapshot(environmentID)
		if !ok {
			return
		}
		state, err := h.fetchRemoteOperationsStateInternal(pollCtx, currentEnvironment)
		if err != nil && isOperationsEndpointMissingInternal(err) {
			// Legacy compatibility: older agents expose dashboard data but not
			// the Operations state endpoint.
			var legacyDashboardState *dashboardtypes.Snapshot
			legacyDashboardState, err = h.fetchRemoteDashboardSnapshotInternal(pollCtx, currentEnvironment, false)
			if err != nil && isDashboardEndpointMissingInternal(err) {
				legacyDashboardState, err = h.fetchLegacyDashboardSnapshotInternal(pollCtx, currentEnvironment)
			}
			if err == nil {
				state = operationsStateFromLegacyDashboardInternal(legacyDashboardState)
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				message, code := classifyOperationsStreamErrorInternal(err)
				agg.Send(ctx, events, operationstypes.StreamEvent{Type: "error", EnvironmentID: environmentID, Error: message, ErrorCode: code, Timestamp: time.Now()})
			}
			return
		}
		filterOperationsStateInternal(ps, environmentID, state)
		agg.Send(ctx, events, operationstypes.StreamEvent{Type: "update", EnvironmentID: environmentID, State: state, Timestamp: time.Now()})
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

func (h *DashboardHandler) fetchRemoteOperationsStateInternal(ctx context.Context, environment models.Environment) (*operationstypes.State, error) {
	var out base.ApiResponse[operationstypes.State]
	if err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet, "/api/environments/0/operations", nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, errors.New("operations state not available")
	}
	return &out.Data, nil
}

func operationsStateFromLegacyDashboardInternal(legacyDashboardState *dashboardtypes.Snapshot) *operationstypes.State {
	result := &operationstypes.State{Compatibility: operationstypes.CompatibilityLegacy}
	if legacyDashboardState == nil {
		return result
	}
	for _, item := range legacyDashboardState.ActionItems.Items {
		count := item.Count
		switch item.Kind {
		case dashboardtypes.ActionItemKindStoppedContainers:
			result.Stopped = &operationstypes.WorkloadCount{Total: count}
		case dashboardtypes.ActionItemKindImageUpdates:
			result.Updates = &operationstypes.WorkloadCount{Total: count}
		case dashboardtypes.ActionItemKindActionableVulnerabilities:
			result.Vulnerabilities = &count
		case dashboardtypes.ActionItemKindExpiringKeys:
			result.ExpiringAPIKeys = &count
		}
	}
	return result
}

func filterOperationsStateInternal(ps *authz.PermissionSet, environmentID string, state *operationstypes.State) {
	if state == nil {
		return
	}
	if !ps.Allows(authz.PermImageUpdatesRead, environmentID) {
		state.Updates = nil
	}
	canReadProjects := ps.Allows(authz.PermProjectsList, environmentID) || ps.Allows(authz.PermProjectsRead, environmentID)
	canReadContainers := ps.Allows(authz.PermContainersList, environmentID) || ps.Allows(authz.PermContainersRead, environmentID)
	if !canReadProjects && !canReadContainers {
		state.Stopped = nil
	} else if state.Stopped != nil {
		if !canReadProjects {
			state.Stopped.Projects = nil
		}
		if !canReadContainers {
			state.Stopped.StandaloneContainers = nil
		}
	}
	if !ps.Allows(authz.PermVulnsRead, environmentID) {
		state.Vulnerabilities = nil
	}
	// API keys are manager-global and must not be repeated for remote agents.
	state.ExpiringAPIKeys = nil
}

func operationsAllowedForEnvironmentInternal(ps *authz.PermissionSet, environmentID string) bool {
	for _, permission := range operationsReadPermissionsInternal {
		if authz.IsEnvScoped(permission) && ps.Allows(permission, environmentID) {
			return true
		}
	}
	return false
}

func isOperationsEndpointMissingInternal(err error) bool {
	if statusErr, ok := errors.AsType[*remenv.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return true
	}
	_, ok := errors.AsType[*remenv.DecodeError](err)
	return ok
}

func classifyOperationsStreamErrorInternal(err error) (string, string) {
	if isOperationsEndpointMissingInternal(err) {
		return "This agent does not support operations summaries", operationstypes.StreamErrorCodeAgentIncompatible
	}
	if transportErr, ok := errors.AsType[*remenv.TransportError](err); ok {
		return transportErr.Error(), operationstypes.StreamErrorCodeUnreachable
	}
	return err.Error(), ""
}
