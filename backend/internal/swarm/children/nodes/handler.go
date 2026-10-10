package nodes

import (
	"context"
	"slices"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/swarm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the swarm node endpoints.
type Handler struct {
	service            *Service
	audit              func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any)
	mapError           func(err error, fallback string) error
	environmentService *environment.EnvironmentService
	cfg                *config.Config
}

func NewHandler(
	service *Service,
	audit func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any),
	mapError func(err error, fallback string) error,
	environmentService *environment.EnvironmentService,
	cfg *config.Config,
) *Handler {
	return &Handler{service: service, audit: audit, mapError: mapError, environmentService: environmentService, cfg: cfg}
}

type ListSwarmNodesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type GetSwarmNodeAgentDeploymentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          struct {
		Rotate bool `json:"rotate,omitempty" doc:"Rotate the environment token before generating snippets"`
	}
}

type SwarmNodeAgentDeployment struct {
	environment.DeploymentSnippet

	EnvironmentID string                `json:"environmentId"`
	Agent         swarm.NodeAgentStatus `json:"agent"`
}

type ReconcileSwarmNodeAgentsInput struct {
	Body          swarm.NodeAgentReconcileRequest
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type PutSwarmNodeAgentBindingInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          swarm.NodeAgentBindingRequest
}

type DeleteSwarmNodeAgentBindingInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type DeleteSwarmNodeAgentDeploymentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type GetSwarmNodeIdentityInput struct{}

type UpdateSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          swarm.NodeUpdateRequest
}

type DeleteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Force         bool   `query:"force" default:"false" doc:"Force node removal"`
}

type PromoteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type DemoteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type ListSwarmNodeTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSwarmJoinCandidatesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type JoinSwarmEnvironmentsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SwarmJoinEnvironmentsRequest
}

// ListNodes lists swarm nodes for an environment and returns a paginated response.
//
// It applies the requested search, sort, and pagination values and guarantees a
// non-nil node slice in the response body.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies the environment ID plus optional filtering and pagination values.
//
// Returns a paginated list of node summaries.
// Returns a mapped HTTP error when node enumeration fails.
func (h *Handler) ListNodes(ctx context.Context, input *ListSwarmNodesInput) (*handlerutil.Page[swarm.NodeSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListNodesPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm nodes: "+err.Error())
	}
	if items == nil {
		items = []swarm.NodeSummary{}
	}

	return &handlerutil.Page[swarm.NodeSummary]{Body: base.Paginated[swarm.NodeSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// GetNode returns detailed information for a single swarm node.
//
// It loads the node through the swarm service and translates not-found
// conditions into the HTTP error returned by the API.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and swarm node to inspect.
//
// Returns a successful response containing the node summary.
// Returns `404 Not Found` when the node does not exist or another mapped HTTP
// error when the inspection fails.
func (h *Handler) GetNode(ctx context.Context, input *GetSwarmNodeInput) (*handlerutil.Out[swarm.NodeSummary], error) {
	node, err := h.service.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm node not found: " + err.Error())
		}
		return nil, h.mapError(err, "Swarm node not found: "+err.Error())
	}

	return &handlerutil.Out[swarm.NodeSummary]{Body: base.ApiResponse[swarm.NodeSummary]{Success: true, Data: *node}}, nil
}

// GetNodeAgentDeployment returns deployment snippets for attaching an Arcane Remote Environment to a swarm node.
//
// It ensures a visible node-bound Remote Environment exists for new
// deployments, reuses legacy hidden registrations when present, optionally
// rotates the environment token, generates the appropriate deployment snippets,
// and refreshes the node summary so the response includes the latest agent
// status.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and node and optionally requests token rotation.
//
// Returns deployment snippets, the backing environment ID, and the refreshed agent status.
// Returns an authorization error for non-admin callers, `401 Unauthorized`
// when the current user cannot be resolved, `404 Not Found` when the node does
// not exist, or `500 Internal Server Error` when environment provisioning or
// snippet generation fails.
func (h *Handler) GetNodeAgentDeployment(ctx context.Context, input *GetSwarmNodeAgentDeploymentInput) (*handlerutil.Out[SwarmNodeAgentDeployment], error) {
	node, err := h.service.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm node not found: " + err.Error())
		}
		return nil, h.mapError(err, "Swarm node not found: "+err.Error())
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	env, apiKey, err := h.environmentService.EnsureSwarmNodeAgentEnvironment(
		ctx,
		input.EnvironmentID,
		input.NodeID,
		node.Hostname,
		user.ID,
		user.Username,
		input.Body.Rotate,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	var snippets *environment.DeploymentSnippets
	if env.IsEdge {
		snippets, err = h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), apiKey, &edge.Config{
			EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
			EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
			EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
			AppURL:            h.cfg.GetAppURL(),
		})
	} else {
		snippets, err = h.environmentService.GenerateDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), env.ApiUrl, apiKey)
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	updatedNode, err := h.service.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[SwarmNodeAgentDeployment]{
		Body: base.ApiResponse[SwarmNodeAgentDeployment]{
			Success: true,
			Data: SwarmNodeAgentDeployment{
				DeploymentSnippet: environment.DeploymentSnippet{
					DockerRun:     snippets.DockerRun,
					DockerCompose: snippets.DockerCompose,
				},
				EnvironmentID: env.ID,
				Agent:         updatedNode.Agent,
			},
		},
	}, nil
}

// ReconcileNodeAgents verifies and persists unique visible-environment node bindings.
func (h *Handler) ReconcileNodeAgents(ctx context.Context, input *ReconcileSwarmNodeAgentsInput) (*handlerutil.Out[swarm.NodeAgentReconcileResponse], error) {
	result, err := h.service.ReconcileNodeAgents(ctx, input.EnvironmentID)
	if err != nil {
		return nil, h.mapError(err, "Failed to reconcile swarm node agents")
	}
	return &handlerutil.Out[swarm.NodeAgentReconcileResponse]{Body: base.ApiResponse[swarm.NodeAgentReconcileResponse]{Success: true, Data: *result}}, nil
}

// PutNodeAgentBinding verifies and attaches an existing visible environment.
func (h *Handler) PutNodeAgentBinding(ctx context.Context, input *PutSwarmNodeAgentBindingInput) (*handlerutil.Out[swarm.NodeSummary], error) {
	if _, err := h.service.BindNodeAgent(ctx, input.EnvironmentID, input.NodeID, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if input.Body.ReplaceDeployment {
		user, err := handlerutil.RequireUser(ctx)
		if err != nil {
			return nil, err
		}
		if deleteSwarmNodeAgentDeploymentErr := h.environmentService.DeleteSwarmNodeAgentDeployment(
			ctx,
			input.EnvironmentID,
			input.NodeID,
			&user.ID,
			&user.Username,
		); deleteSwarmNodeAgentDeploymentErr != nil {
			return nil, huma.Error500InternalServerError(deleteSwarmNodeAgentDeploymentErr.Error())
		}
	}

	node, err := h.service.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		return nil, h.mapError(err, "Failed to refresh swarm node binding")
	}
	return &handlerutil.Out[swarm.NodeSummary]{Body: base.ApiResponse[swarm.NodeSummary]{Success: true, Data: *node}}, nil
}

// DeleteNodeAgentBinding detaches the visible environment currently bound to a node.
func (h *Handler) DeleteNodeAgentBinding(ctx context.Context, input *DeleteSwarmNodeAgentBindingInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.environmentService.DetachSwarmNodeEnvironment(ctx, input.EnvironmentID, input.NodeID); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return handlerutil.MessageOutput("Swarm node environment detached", ""), nil
}

// DeleteNodeAgentDeployment removes a dedicated hidden node-agent registration.
func (h *Handler) DeleteNodeAgentDeployment(ctx context.Context, input *DeleteSwarmNodeAgentDeploymentInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if deleteSwarmNodeAgentDeploymentErr := h.environmentService.DeleteSwarmNodeAgentDeployment(
		ctx,
		input.EnvironmentID,
		input.NodeID,
		&user.ID,
		&user.Username,
	); deleteSwarmNodeAgentDeploymentErr != nil {
		return nil, huma.Error500InternalServerError(deleteSwarmNodeAgentDeploymentErr.Error())
	}
	return handlerutil.MessageOutput("Dedicated swarm node agent registration removed", ""), nil
}

// GetNodeIdentity returns the swarm identity of the node serving the current request.
//
// It is used by edge agents and local nodes to report their swarm node ID,
// hostname, role, engine version, and swarm participation state.
//
// ctx carries request-scoped cancellation and auth context.
// The input value is unused because the endpoint has no parameters.
//
// Returns the local swarm node identity when it can be determined.
// Returns `500 Internal Server Error` when the swarm service is unavailable or
// identity discovery fails.
func (h *Handler) GetNodeIdentity(ctx context.Context, _ *GetSwarmNodeIdentityInput) (*handlerutil.Out[SwarmNodeIdentity], error) {
	identity, err := h.service.GetLocalNodeIdentity(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[SwarmNodeIdentity]{
		Body: base.ApiResponse[SwarmNodeIdentity]{
			Success: true,
			Data:    *identity,
		},
	}, nil
}

// UpdateNode updates mutable settings on a swarm node.
//
// It requires admin privileges, forwards the requested node changes to the
// swarm service, and records an audit event when the mutation succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to update and contains the requested changes.
//
// Returns a confirmation response when the update succeeds.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the node update fails.
func (h *Handler) UpdateNode(ctx context.Context, input *UpdateSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.UpdateNode(ctx, input.NodeID, input.Body); err != nil {
		return nil, h.mapError(err, "Swarm node not found: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "node.update", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return handlerutil.MessageOutput("Swarm node updated successfully", ""), nil
}

// DeleteNode removes a swarm node from the cluster.
//
// It requires admin privileges, supports forced removal when requested, and
// records the deletion parameters in the audit event metadata.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to remove and indicates whether removal should be forced.
//
// Returns a confirmation response when the node is removed.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the node cannot be removed.
func (h *Handler) DeleteNode(ctx context.Context, input *DeleteSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.RemoveNode(ctx, input.NodeID, input.Force); err != nil {
		return nil, h.mapError(err, "Swarm node not found: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "node.delete", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID, "force": input.Force})

	return handlerutil.MessageOutput("Swarm node removed successfully", ""), nil
}

// PromoteNode promotes a swarm worker to manager.
//
// It requires admin privileges, performs the promotion through the swarm
// service, and records an audit event after the role change completes.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to promote.
//
// Returns a confirmation response when the promotion succeeds.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the promotion fails.
func (h *Handler) PromoteNode(ctx context.Context, input *PromoteSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.PromoteNode(ctx, input.NodeID); err != nil {
		return nil, h.mapError(err, "Swarm node not found: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "node.promote", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return handlerutil.MessageOutput("Swarm node promoted successfully", ""), nil
}

// DemoteNode demotes a swarm manager to worker.
//
// It requires admin privileges, performs the demotion through the swarm
// service, and records an audit event after the role change completes.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to demote.
//
// Returns a confirmation response when the demotion succeeds.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the demotion fails.
func (h *Handler) DemoteNode(ctx context.Context, input *DemoteSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.DemoteNode(ctx, input.NodeID); err != nil {
		return nil, h.mapError(err, "Swarm node not found: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "node.demote", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return handlerutil.MessageOutput("Swarm node demoted successfully", ""), nil
}

// ListNodeTasks lists tasks currently associated with a swarm node.
//
// It applies search, sort, and pagination inputs and normalizes nil task lists
// to empty arrays in the API response.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the node and provides optional filtering and pagination values.
//
// Returns a paginated list of node task summaries.
// Returns a mapped HTTP error when the underlying lookup fails.
func (h *Handler) ListNodeTasks(ctx context.Context, input *ListSwarmNodeTasksInput) (*handlerutil.Page[swarm.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListNodeTasksPaginated(ctx, input.NodeID, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm tasks: "+err.Error())
	}
	if items == nil {
		items = []swarm.TaskSummary{}
	}

	return &handlerutil.Page[swarm.TaskSummary]{Body: base.Paginated[swarm.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// GetJoinCandidates lists enabled visible environments the caller can join.
func (h *Handler) GetJoinCandidates(ctx context.Context, input *GetSwarmJoinCandidatesInput) (*handlerutil.Out[[]swarm.SwarmJoinCandidate], error) {
	if err := requireEasyJoinManagerPermissionsInternal(ctx, input.EnvironmentID); err != nil {
		return nil, err
	}
	candidates, err := h.service.GetJoinCandidates(ctx, input.EnvironmentID)
	if err != nil {
		return nil, h.mapError(err, "Failed to list Easy Join candidates")
	}
	permissions, _ := middleware.PermissionsFromContext(ctx)
	candidates = slices.DeleteFunc(candidates, func(candidate swarm.SwarmJoinCandidate) bool {
		return !permissions.Allows(authz.PermSwarmJoin, candidate.EnvironmentID)
	})

	return &handlerutil.Out[[]swarm.SwarmJoinCandidate]{Body: base.ApiResponse[[]swarm.SwarmJoinCandidate]{Success: true, Data: candidates}}, nil
}

// JoinEnvironments performs Easy Join without returning manager join tokens.
func (h *Handler) JoinEnvironments(ctx context.Context, input *JoinSwarmEnvironmentsInput) (*handlerutil.Out[swarm.SwarmJoinEnvironmentsResponse], error) {
	if err := requireEasyJoinManagerPermissionsInternal(ctx, input.EnvironmentID); err != nil {
		return nil, err
	}

	permissions, _ := middleware.PermissionsFromContext(ctx)
	for _, target := range input.Body.Targets {
		if target.EnvironmentID == input.EnvironmentID {
			return nil, huma.Error400BadRequest("selected swarm manager cannot also be a join target")
		}
		if target.Role != swarm.SwarmJoinEnvironmentRoleWorker && target.Role != swarm.SwarmJoinEnvironmentRoleManager {
			return nil, huma.Error400BadRequest("join target role must be worker or manager")
		}
		if permissions == nil || !permissions.Allows(authz.PermSwarmJoin, target.EnvironmentID) {
			return nil, huma.Error403Forbidden("swarm:join permission is required for every target environment")
		}
	}

	result, err := h.service.JoinEnvironments(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to join swarm environments")
	}
	return &handlerutil.Out[swarm.SwarmJoinEnvironmentsResponse]{Body: base.ApiResponse[swarm.SwarmJoinEnvironmentsResponse]{Success: true, Data: *result}}, nil
}

func requireEasyJoinManagerPermissionsInternal(ctx context.Context, environmentID string) error {
	permissions, _ := middleware.PermissionsFromContext(ctx)
	if permissions == nil || !permissions.Allows(authz.PermSwarmJoin, environmentID) {
		return huma.Error403Forbidden("swarm:join permission is required on the manager environment")
	}
	return nil
}
