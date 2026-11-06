package swarm

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/swarm/children/nodes"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	SwarmHandler struct {
		swarmService *SwarmService
		eventService *event.EventService
	}

	ListSwarmTasksInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	}

	InitSwarmInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmInitRequest
	}

	JoinSwarmInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmJoinRequest
	}

	LeaveSwarmInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmLeaveRequest
	}

	UnlockSwarmInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmUnlockRequest
	}

	SwarmJoinTokensActionInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Action        string `query:"action" required:"true" enum:"rotate" doc:"Join token action"`
		Body          types.SwarmRotateJoinTokensRequest
	}

	UpdateSwarmSpecInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmUpdateRequest
	}
)

func NewHandler(swarmService *SwarmService, eventService *event.EventService) *SwarmHandler {
	return &SwarmHandler{swarmService: swarmService, eventService: eventService}
}

func (h *SwarmHandler) ListTasks(ctx context.Context, input *ListSwarmTasksInput) (*handlerutil.Page[types.SwarmTaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.swarmService.AggregateTasks(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list swarm tasks: " + err.Error())
		}
		return &handlerutil.Page[types.SwarmTaskSummary]{Body: body}, nil
	}
	items, paginationResp, err := h.swarmService.ListTasksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to list swarm tasks: "+err.Error())
	}

	return &handlerutil.Page[types.SwarmTaskSummary]{Body: types.Paginated[types.SwarmTaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

func (h *SwarmHandler) GetSwarmStatus(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[types.SwarmRuntimeStatus], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.swarmService.AggregateStatus(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to read swarm status")
		}
		return &handlerutil.Out[types.SwarmRuntimeStatus]{Body: body}, nil
	}

	status, err := h.swarmService.GetStatus(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read swarm status")
	}

	return &handlerutil.Out[types.SwarmRuntimeStatus]{
		Body: types.ApiResponse[types.SwarmRuntimeStatus]{
			Success: true,
			Data:    status,
		},
	}, nil
}

func (h *SwarmHandler) GetSwarmInfo(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[types.SwarmInfo], error) {
	info, err := h.swarmService.GetSwarmInfo(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm: "+err.Error())
	}

	return &handlerutil.Out[types.SwarmInfo]{Body: types.ApiResponse[types.SwarmInfo]{Success: true, Data: *info}}, nil
}

func (h *SwarmHandler) InitSwarm(ctx context.Context, input *InitSwarmInput) (*handlerutil.Out[types.SwarmInitResponse], error) {
	resp, err := h.swarmService.InitSwarm(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to initialize swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.init", "swarm", "cluster", "cluster", map[string]any{"nodeId": resp.NodeID})

	return &handlerutil.Out[types.SwarmInitResponse]{Body: types.ApiResponse[types.SwarmInitResponse]{Success: true, Data: *resp}}, nil
}

func (h *SwarmHandler) JoinSwarm(ctx context.Context, input *JoinSwarmInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.swarmService.JoinSwarm(ctx, input.Body); err != nil {
		detail := nodes.RedactSwarmJoinToken(err.Error(), input.Body.JoinToken)
		slog.WarnContext(ctx, "swarm join failed", "environmentId", input.EnvironmentID, "operation", "join-swarm", "error", detail)
		httpErr := mapSwarmServiceError(err, detail)
		if model, ok := errors.AsType[*huma.ErrorModel](httpErr); ok {
			model.Detail = nodes.RedactSwarmJoinToken(model.Detail, input.Body.JoinToken)
		}
		return nil, httpErr
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.join", "swarm", "cluster", "cluster", map[string]any{"remoteAddrs": input.Body.RemoteAddrs})

	return handlerutil.MessageOutput("Joined swarm successfully", ""), nil
}

func (h *SwarmHandler) LeaveSwarm(ctx context.Context, input *LeaveSwarmInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.swarmService.LeaveSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to leave swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.leave", "swarm", "cluster", "cluster", map[string]any{"force": input.Body.Force})

	return handlerutil.MessageOutput("Left swarm successfully", ""), nil
}

func (h *SwarmHandler) UnlockSwarm(ctx context.Context, input *UnlockSwarmInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.swarmService.UnlockSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to unlock swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.unlock", "swarm", "cluster", "cluster", map[string]any{})

	return handlerutil.MessageOutput("Swarm unlocked successfully", ""), nil
}

func (h *SwarmHandler) GetUnlockKey(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[types.SwarmUnlockKeyResponse], error) {
	resp, err := h.swarmService.GetSwarmUnlockKey(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to get swarm unlock key")
	}

	return &handlerutil.Out[types.SwarmUnlockKeyResponse]{Body: types.ApiResponse[types.SwarmUnlockKeyResponse]{Success: true, Data: *resp}}, nil
}

func (h *SwarmHandler) GetJoinTokens(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[types.SwarmJoinTokensResponse], error) {
	resp, err := h.swarmService.GetSwarmJoinTokens(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to get swarm join tokens")
	}

	return &handlerutil.Out[types.SwarmJoinTokensResponse]{Body: types.ApiResponse[types.SwarmJoinTokensResponse]{Success: true, Data: *resp}}, nil
}

func (h *SwarmHandler) RotateJoinTokens(ctx context.Context, input *SwarmJoinTokensActionInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.swarmService.RotateSwarmJoinTokens(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to rotate swarm join tokens")
	}

	h.auditSwarmMutation(
		ctx,
		input.EnvironmentID,
		"lifecycle.rotate_tokens",
		"swarm",
		"cluster",
		"cluster",
		map[string]any{
			"rotateWorker":  input.Body.RotateWorkerToken,
			"rotateManager": input.Body.RotateManagerToken,
		},
	)

	return handlerutil.MessageOutput("Swarm join tokens rotated successfully", ""), nil
}

func (h *SwarmHandler) UpdateSwarmSpec(ctx context.Context, input *UpdateSwarmSpecInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.swarmService.UpdateSwarmSpec(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to update swarm spec")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.update_spec", "swarm", "cluster", "cluster", map[string]any{})

	return handlerutil.MessageOutput("Swarm spec updated successfully", ""), nil
}

func (h *SwarmHandler) auditSwarmMutation(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any) {
	var userID *string
	var username *string
	if user, ok := handlerutil.CurrentUserFromContext(ctx); ok {
		userID = new(user.ID)
		username = new(user.Username)
	}

	var resourceTypePtr *string
	if strings.TrimSpace(resourceType) != "" {
		resourceTypePtr = new(resourceType)
	}
	var resourceIDPtr *string
	if strings.TrimSpace(resourceID) != "" {
		resourceIDPtr = new(resourceID)
	}
	var resourceNamePtr *string
	if strings.TrimSpace(resourceName) != "" {
		resourceNamePtr = new(resourceName)
	}

	env := cmp.Or(strings.TrimSpace(environmentID), "0")
	envPtr := &env

	meta := database.JSON{"action": action}
	maps.Copy(meta, metadata)

	_, err := h.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventType("swarm." + action),
		Severity:      event.EventSeverityInfo,
		Title:         "Swarm operation: " + action,
		Description:   "Swarm operation '" + action + "' completed",
		ResourceType:  resourceTypePtr,
		ResourceID:    resourceIDPtr,
		ResourceName:  resourceNamePtr,
		UserID:        userID,
		Username:      username,
		EnvironmentID: envPtr,
		Metadata:      meta,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to audit swarm mutation", "action", action, "error", err)
	}
}

func mapSwarmServiceError(err error, fallback string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, common.ErrSwarmNotEnabled) {
		return huma.Error409Conflict("Swarm mode is not enabled")
	}
	if errors.Is(err, common.ErrSwarmManagerRequired) {
		return huma.Error403Forbidden("Swarm manager access required")
	}
	if errors.Is(err, common.ErrBadRequest) {
		return huma.Error400BadRequest(err.Error())
	}
	if errdefs.IsNotFound(err) || errors.Is(err, common.ErrNotFound) {
		return huma.Error404NotFound(err.Error())
	}
	if errdefs.IsInvalidArgument(err) {
		return huma.Error400BadRequest(err.Error())
	}
	if errdefs.IsConflict(err) || errors.Is(err, common.ErrConflict) {
		return huma.Error409Conflict(err.Error())
	}
	if remoteStatus, ok := errors.AsType[*agent.StatusError](err); ok && remoteStatus.StatusCode >= 400 && remoteStatus.StatusCode <= 599 {
		return huma.NewError(remoteStatus.StatusCode, fallback)
	}
	errText := strings.ToLower(err.Error())
	if strings.Contains(errText, "required") || strings.Contains(errText, "invalid") {
		return huma.Error400BadRequest(err.Error())
	}
	return huma.Error500InternalServerError(fallback)
}
