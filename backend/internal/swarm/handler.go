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
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/swarm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm/children/nodes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

type SwarmHandler struct {
	swarmService *SwarmService
	eventService *event.EventService
}

type ListSwarmTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSwarmInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type InitSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmInitRequest
}

type JoinSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmJoinRequest
}

type LeaveSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmLeaveRequest
}

type UnlockSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmUnlockRequest
}

type GetSwarmUnlockKeyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmJoinTokensInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type RotateSwarmJoinTokensInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmRotateJoinTokensRequest
}

type UpdateSwarmSpecInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmUpdateRequest
}

func NewHandler(swarmService *SwarmService, eventService *event.EventService) *SwarmHandler {
	return &SwarmHandler{swarmService: swarmService, eventService: eventService}
}

// ListTasks lists swarm tasks across the current environment.
//
// It applies the requested search, sort, and pagination fields and guarantees
// an empty task slice when no tasks are returned.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies optional filtering and pagination values.
//
// Returns a paginated task listing for the environment.
// Returns a mapped HTTP error when task enumeration fails.
func (h *SwarmHandler) ListTasks(ctx context.Context, input *ListSwarmTasksInput) (*handlerutil.Page[swarm.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListTasksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to list swarm tasks: "+err.Error())
	}
	if items == nil {
		items = []swarm.TaskSummary{}
	}

	return &handlerutil.Page[swarm.TaskSummary]{Body: base.Paginated[swarm.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// GetSwarmStatus returns the current swarm cluster metadata for an environment.
//
// It delegates to the swarm service to inspect the local swarm state and maps
// service-layer failures to the API's HTTP error model.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose swarm metadata should be returned.
//
// Returns the current swarm information when swarm mode is available.
// Returns a mapped HTTP error when swarm inspection fails.
func (h *SwarmHandler) GetSwarmStatus(ctx context.Context, input *GetSwarmStatusInput) (*handlerutil.Out[swarm.RuntimeStatus], error) {
	enabled, err := h.swarmService.IsEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read swarm status")
	}

	return &handlerutil.Out[swarm.RuntimeStatus]{
		Body: base.ApiResponse[swarm.RuntimeStatus]{
			Success: true,
			Data:    swarm.RuntimeStatus{Enabled: enabled},
		},
	}, nil
}

// GetSwarmInfo returns the current swarm cluster metadata for an environment.
//
// It delegates to the swarm service to inspect the local swarm state and maps
// service-layer failures to the API's HTTP error model.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose swarm metadata should be returned.
//
// Returns the current swarm information when swarm mode is available.
// Returns a mapped HTTP error when swarm inspection fails.
func (h *SwarmHandler) GetSwarmInfo(ctx context.Context, input *GetSwarmInfoInput) (*handlerutil.Out[swarm.SwarmInfo], error) {
	info, err := h.swarmService.GetSwarmInfo(ctx)
	if err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to inspect swarm: "+err.Error())
	}

	return &handlerutil.Out[swarm.SwarmInfo]{Body: base.ApiResponse[swarm.SwarmInfo]{Success: true, Data: *info}}, nil
}

// InitSwarm initializes swarm mode on the target engine.
//
// It requires admin privileges, delegates the initialization request to the
// swarm service, and records an audit event that includes the created node ID.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the swarm initialization request body.
//
// Returns the initialized swarm node ID and any other initialization details.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when initialization fails.
func (h *SwarmHandler) InitSwarm(ctx context.Context, input *InitSwarmInput) (*handlerutil.Out[swarm.SwarmInitResponse], error) {
	resp, err := h.swarmService.InitSwarm(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to initialize swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.init", "swarm", "cluster", "cluster", map[string]any{"nodeId": resp.NodeID})

	return &handlerutil.Out[swarm.SwarmInitResponse]{Body: base.ApiResponse[swarm.SwarmInitResponse]{Success: true, Data: *resp}}, nil
}

// JoinSwarm joins the target engine to an existing swarm cluster.
//
// It requires admin privileges, forwards the join request to the swarm service,
// and records the remote manager addresses in the audit metadata.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the join request body.
//
// Returns a confirmation response when the engine joins successfully.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the join operation fails.
func (h *SwarmHandler) JoinSwarm(ctx context.Context, input *JoinSwarmInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.JoinSwarm(ctx, input.Body); err != nil {
		detail := nodes.RedactSwarmJoinToken(err.Error(), input.Body.JoinToken)
		slog.WarnContext(ctx, "swarm join failed", "environmentId", input.EnvironmentID, "operation", "join-swarm", "error", detail)
		httpErr := mapSwarmServiceErrorInternal(err, detail)
		if model, ok := errors.AsType[*huma.ErrorModel](httpErr); ok {
			model.Detail = nodes.RedactSwarmJoinToken(model.Detail, input.Body.JoinToken)
		}
		return nil, httpErr
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.join", "swarm", "cluster", "cluster", map[string]any{"remoteAddrs": input.Body.RemoteAddrs})

	return handlerutil.MessageOutput("Joined swarm successfully", ""), nil
}

// LeaveSwarm removes the target engine from its current swarm cluster.
//
// It requires admin privileges, forwards the leave request to the swarm
// service, and records whether forced removal was requested.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the leave request body.
//
// Returns a confirmation response when the engine leaves successfully.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the leave operation fails.
func (h *SwarmHandler) LeaveSwarm(ctx context.Context, input *LeaveSwarmInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.LeaveSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to leave swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.leave", "swarm", "cluster", "cluster", map[string]any{"force": input.Body.Force})

	return handlerutil.MessageOutput("Left swarm successfully", ""), nil
}

// UnlockSwarm unlocks a swarm manager using the supplied unlock key.
//
// It requires admin privileges, delegates the unlock request to the swarm
// service, and emits an audit event after success.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the unlock request body.
//
// Returns a confirmation response when the manager is unlocked.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the unlock operation fails.
func (h *SwarmHandler) UnlockSwarm(ctx context.Context, input *UnlockSwarmInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.UnlockSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to unlock swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.unlock", "swarm", "cluster", "cluster", map[string]any{})

	return handlerutil.MessageOutput("Swarm unlocked successfully", ""), nil
}

// GetUnlockKey returns the current swarm manager unlock key.
//
// It delegates to the swarm service and exposes the unlock key in the standard
// API response envelope.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose unlock key should be returned.
//
// Returns the current manager unlock key.
// Returns a mapped HTTP error when the unlock key cannot be retrieved.
func (h *SwarmHandler) GetUnlockKey(ctx context.Context, input *GetSwarmUnlockKeyInput) (*handlerutil.Out[swarm.SwarmUnlockKeyResponse], error) {
	resp, err := h.swarmService.GetSwarmUnlockKey(ctx)
	if err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to get swarm unlock key")
	}

	return &handlerutil.Out[swarm.SwarmUnlockKeyResponse]{Body: base.ApiResponse[swarm.SwarmUnlockKeyResponse]{Success: true, Data: *resp}}, nil
}

// GetJoinTokens returns the current swarm worker and manager join tokens.
//
// It delegates to the swarm service and wraps the returned tokens in the
// standard API response shape.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose join tokens should be returned.
//
// Returns the current worker and manager join tokens.
// Returns a mapped HTTP error when token lookup fails.
func (h *SwarmHandler) GetJoinTokens(ctx context.Context, input *GetSwarmJoinTokensInput) (*handlerutil.Out[swarm.SwarmJoinTokensResponse], error) {
	resp, err := h.swarmService.GetSwarmJoinTokens(ctx)
	if err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to get swarm join tokens")
	}

	return &handlerutil.Out[swarm.SwarmJoinTokensResponse]{Body: base.ApiResponse[swarm.SwarmJoinTokensResponse]{Success: true, Data: *resp}}, nil
}

// RotateJoinTokens rotates the swarm worker and or manager join tokens.
//
// It requires admin privileges, delegates the rotation request to the swarm
// service, and records which token classes were rotated.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the requested token-rotation flags.
//
// Returns a confirmation response when rotation succeeds.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when token rotation fails.
func (h *SwarmHandler) RotateJoinTokens(ctx context.Context, input *RotateSwarmJoinTokensInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RotateSwarmJoinTokens(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to rotate swarm join tokens")
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

// UpdateSwarmSpec updates the swarm cluster specification.
//
// It requires admin privileges, forwards the request to the swarm service, and
// records an audit event after the spec change succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the replacement swarm spec.
//
// Returns a confirmation response when the spec update succeeds.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the spec update fails.
func (h *SwarmHandler) UpdateSwarmSpec(ctx context.Context, input *UpdateSwarmSpecInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.UpdateSwarmSpec(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceErrorInternal(err, "Failed to update swarm spec")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.update_spec", "swarm", "cluster", "cluster", map[string]any{})

	return handlerutil.MessageOutput("Swarm spec updated successfully", ""), nil
}

// auditSwarmMutation writes an informational event for a completed swarm mutation.
//
// It enriches the event with the current user when available, normalizes blank
// environment IDs to the local environment, and logs a warning instead of
// failing the request when event creation is unsuccessful.
//
// ctx carries request-scoped cancellation and user context.
// environmentID identifies the environment associated with the mutation.
// action names the performed swarm action.
// resourceType classifies the mutated resource for the audit trail.
// resourceID identifies the mutated resource when one exists.
// resourceName provides a human-readable resource name when one exists.
// metadata supplies additional structured audit fields to attach to the event.
func (h *SwarmHandler) auditSwarmMutation(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any) {
	if h.eventService == nil {
		return
	}

	var userID *string
	var username *string
	if user, ok := userctx.CurrentUserFromContext(ctx); ok {
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

// mapSwarmServiceErrorInternal converts swarm-service errors into Huma HTTP errors.
//
// It recognizes Arcane's swarm sentinel errors, common Docker error classes,
// and a small set of validation-like substrings before falling back to an
// internal-server-error response.
//
// err is the original service-layer error to translate.
// fallback is the generic message returned when no specific mapping applies.
//
// Returns an HTTP-shaped error suitable for returning from a Huma handler.
func mapSwarmServiceErrorInternal(err error, fallback string) error {
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
	if errdefs.IsNotFound(err) {
		return huma.Error404NotFound(err.Error())
	}
	if errdefs.IsInvalidArgument(err) {
		return huma.Error400BadRequest(err.Error())
	}
	if errdefs.IsConflict(err) {
		return huma.Error409Conflict(err.Error())
	}
	if remoteStatus, ok := errors.AsType[*remenv.StatusError](err); ok && remoteStatus.StatusCode >= 400 && remoteStatus.StatusCode <= 599 {
		return huma.NewError(remoteStatus.StatusCode, fallback)
	}
	errText := strings.ToLower(err.Error())
	if strings.Contains(errText, "required") || strings.Contains(errText, "invalid") {
		return huma.Error400BadRequest(err.Error())
	}
	return huma.Error500InternalServerError(fallback)
}
