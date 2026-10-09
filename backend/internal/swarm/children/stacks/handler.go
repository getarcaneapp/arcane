package stacks

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/swarm"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the swarm stack endpoints.
type Handler struct {
	service  *Service
	audit    func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any)
	mapError func(err error, fallback string) error
}

func NewHandler(
	service *Service,
	audit func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any),
	mapError func(err error, fallback string) error,
) *Handler {
	return &Handler{service: service, audit: audit, mapError: mapError}
}

type ListSwarmStacksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type DeploySwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.StackDeployRequest
}

type GetSwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type GetSwarmStackSourceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type UpdateSwarmStackSourceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
	Body          swarm.StackSourceUpdateRequest
}

type DeleteSwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type ListSwarmStackServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmStackTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type RenderSwarmStackConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.StackRenderConfigRequest
}

// ListStacks lists swarm stacks for the current environment.
//
// It applies search, sort, and pagination values supplied by the caller and
// returns an empty stack slice instead of nil when no stacks are present.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies optional filtering and pagination values.
//
// Returns a paginated list of stack summaries.
// Returns a mapped HTTP error when stack enumeration fails.
func (h *Handler) ListStacks(ctx context.Context, input *ListSwarmStacksInput) (*handlerutil.Page[swarm.StackSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListStacksPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm stacks: "+err.Error())
	}
	if items == nil {
		items = []swarm.StackSummary{}
	}

	return &handlerutil.Page[swarm.StackSummary]{Body: base.Paginated[swarm.StackSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// DeployStack deploys or updates a swarm stack.
//
// It requires admin privileges, submits the stack deployment request to the
// swarm service, and records an audit event keyed by the stack name after the
// deployment succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the target environment and provides the stack deployment request body.
//
// Returns the deployment response reported by the swarm service.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when rendering, validation, or deployment fails.
func (h *Handler) DeployStack(ctx context.Context, input *DeploySwarmStackInput) (*handlerutil.Out[swarm.StackDeployResponse], error) {
	resp, err := h.service.DeployStack(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to deploy swarm stack: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "stack.deploy", "swarm_stack", input.Body.Name, input.Body.Name, map[string]any{"stack": input.Body.Name})

	return &handlerutil.Out[swarm.StackDeployResponse]{Body: base.ApiResponse[swarm.StackDeployResponse]{Success: true, Data: *resp}}, nil
}

// GetStack returns detailed information for a specific swarm stack.
//
// It looks up the stack by name through the swarm service and maps missing
// stacks to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and stack name to inspect.
//
// Returns the stack inspection payload when the stack exists.
// Returns `404 Not Found` when the stack does not exist or another mapped HTTP
// error when inspection fails.
func (h *Handler) GetStack(ctx context.Context, input *GetSwarmStackInput) (*handlerutil.Out[swarm.StackInspect], error) {
	stack, err := h.service.GetStack(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to inspect swarm stack")
	}

	return &handlerutil.Out[swarm.StackInspect]{Body: base.ApiResponse[swarm.StackInspect]{Success: true, Data: *stack}}, nil
}

// GetStackSource returns the stored source content for a swarm stack.
//
// It requires admin privileges because stack source content can include
// sensitive configuration, and it maps missing stack sources to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and stack whose saved source should be loaded.
//
// Returns the stored compose and environment source for the stack.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// no saved source exists, or another mapped HTTP error when loading fails.
func (h *Handler) GetStackSource(ctx context.Context, input *GetSwarmStackSourceInput) (*handlerutil.Out[swarm.StackSource], error) {
	source, err := h.service.GetStackSource(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack source not found")
		}
		return nil, h.mapError(err, "Failed to load swarm stack source")
	}

	return &handlerutil.Out[swarm.StackSource]{Body: base.ApiResponse[swarm.StackSource]{Success: true, Data: *source}}, nil
}

// UpdateStackSource persists the saved compose and env source for a swarm
// stack and redeploys the stack so the edit takes effect on running services.
//
// It requires admin privileges because stack source content can include
// sensitive configuration. The stack name comes from the route, and the body
// contains the replacement source files to save.
func (h *Handler) UpdateStackSource(ctx context.Context, input *UpdateSwarmStackSourceInput) (*handlerutil.Out[swarm.StackSource], error) {
	source, err := h.service.UpdateStackSource(ctx, input.EnvironmentID, input.Name, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm stack source")
	}

	h.audit(ctx, input.EnvironmentID, "stack.source.update", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return &handlerutil.Out[swarm.StackSource]{Body: base.ApiResponse[swarm.StackSource]{Success: true, Data: *source}}, nil
}

// DeleteStack removes a swarm stack and its managed resources.
//
// It requires admin privileges, delegates the removal to the swarm service,
// maps missing stacks to `404 Not Found`, and records an audit event after
// deletion completes.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and stack name to remove.
//
// Returns a confirmation response when the stack is removed.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the stack does not exist, or another mapped HTTP error when removal fails.
func (h *Handler) DeleteStack(ctx context.Context, input *DeleteSwarmStackInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.RemoveStack(ctx, input.EnvironmentID, input.Name); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to remove swarm stack")
	}

	h.audit(ctx, input.EnvironmentID, "stack.delete", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return handlerutil.MessageOutput("Swarm stack removed successfully", ""), nil
}

// ListStackServices lists services belonging to a swarm stack.
//
// It applies search, sort, and pagination options, ensures the response uses an
// empty slice instead of nil, and maps missing stacks to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the stack and provides optional filtering and pagination fields.
//
// Returns a paginated list of service summaries for the stack.
// Returns `404 Not Found` when the stack does not exist or another mapped HTTP
// error when the lookup fails.
func (h *Handler) ListStackServices(ctx context.Context, input *ListSwarmStackServicesInput) (*handlerutil.Page[swarm.ServiceSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListStackServicesPaginated(ctx, input.Name, params)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to list swarm stack services")
	}
	if items == nil {
		items = []swarm.ServiceSummary{}
	}

	return &handlerutil.Page[swarm.ServiceSummary]{Body: base.Paginated[swarm.ServiceSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// ListStackTasks lists tasks belonging to a swarm stack.
//
// It applies search, sort, and pagination options, ensures the response uses an
// empty slice instead of nil, and maps missing stacks to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the stack and provides optional filtering and pagination fields.
//
// Returns a paginated list of task summaries for the stack.
// Returns `404 Not Found` when the stack does not exist or another mapped HTTP
// error when the lookup fails.
func (h *Handler) ListStackTasks(ctx context.Context, input *ListSwarmStackTasksInput) (*handlerutil.Page[swarm.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListStackTasksPaginated(ctx, input.Name, params)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to list swarm stack tasks")
	}
	if items == nil {
		items = []swarm.TaskSummary{}
	}

	return &handlerutil.Page[swarm.TaskSummary]{Body: base.Paginated[swarm.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// RenderStackConfig renders and validates a swarm stack configuration without deploying it.
//
// It delegates to the swarm service to parse the provided compose and
// environment content and returns the normalized render result.
//
// ctx carries request-scoped cancellation and auth context.
// input provides the stack render request body.
//
// Returns the rendered compose content together with discovered resource names.
// Returns a mapped HTTP error when parsing, interpolation, or rendering fails.
func (h *Handler) RenderStackConfig(ctx context.Context, input *RenderSwarmStackConfigInput) (*handlerutil.Out[swarm.StackRenderConfigResponse], error) {
	resp, err := h.service.RenderStackConfig(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to render swarm stack config")
	}

	return &handlerutil.Out[swarm.StackRenderConfigResponse]{Body: base.ApiResponse[swarm.StackRenderConfigResponse]{Success: true, Data: *resp}}, nil
}
