package stacks

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service  *Service
		audit    func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any)
		mapError func(err error, fallback string) error
	}

	ListSwarmStacksInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	}

	DeploySwarmStackInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmStackDeployRequest
	}

	GetSwarmStackInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Name          string `path:"name" doc:"Stack name"`
	}

	GetSwarmStackSourceInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Name          string `path:"name" doc:"Stack name"`
	}

	UpdateSwarmStackSourceInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Name          string `path:"name" doc:"Stack name"`
		types.Update[types.SwarmStackSource]
	}

	DeleteSwarmStackInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Name          string `path:"name" doc:"Stack name"`
	}

	ListSwarmStackServicesInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Name          string `path:"name" doc:"Stack name"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	}

	ListSwarmStackTasksInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Name          string `path:"name" doc:"Stack name"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	}

	RenderSwarmStackConfigInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.SwarmStackRenderConfigRequest
	}
)

func NewHandler(
	service *Service,
	audit func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any),
	mapError func(err error, fallback string) error,
) *Handler {
	return &Handler{service: service, audit: audit, mapError: mapError}
}

func (h *Handler) ListStacks(ctx context.Context, input *ListSwarmStacksInput) (*handlerutil.Page[types.SwarmStackSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list swarm stacks: " + err.Error())
		}
		return &handlerutil.Page[types.SwarmStackSummary]{Body: body}, nil
	}
	items, paginationResp, err := h.service.ListStacksPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm stacks: "+err.Error())
	}

	return &handlerutil.Page[types.SwarmStackSummary]{Body: types.Paginated[types.SwarmStackSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

func (h *Handler) DeployStack(ctx context.Context, input *DeploySwarmStackInput) (*handlerutil.Out[types.SwarmStackDeployResponse], error) {
	resp, err := h.service.DeployStack(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to deploy swarm stack: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "stack.deploy", "swarm_stack", input.Body.Name, input.Body.Name, map[string]any{"stack": input.Body.Name})

	return &handlerutil.Out[types.SwarmStackDeployResponse]{Body: types.ApiResponse[types.SwarmStackDeployResponse]{Success: true, Data: *resp}}, nil
}

func (h *Handler) GetStack(ctx context.Context, input *GetSwarmStackInput) (*handlerutil.Out[types.SwarmStackInspect], error) {
	stack, err := h.service.GetStack(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to inspect swarm stack")
	}

	return &handlerutil.Out[types.SwarmStackInspect]{Body: types.ApiResponse[types.SwarmStackInspect]{Success: true, Data: *stack}}, nil
}

func (h *Handler) GetStackSource(ctx context.Context, input *GetSwarmStackSourceInput) (*handlerutil.Out[types.SwarmStackSource], error) {
	source, err := h.service.GetStackSource(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack source not found")
		}
		return nil, h.mapError(err, "Failed to load swarm stack source")
	}

	return &handlerutil.Out[types.SwarmStackSource]{Body: types.ApiResponse[types.SwarmStackSource]{Success: true, Data: *source}}, nil
}

func (h *Handler) UpdateStackSource(ctx context.Context, input *UpdateSwarmStackSourceInput) (*handlerutil.Out[types.SwarmStackSource], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}

	source, err := h.service.UpdateStackSource(ctx, input.EnvironmentID, input.Name, StackSourceUpdate{
		Source:    input.Body,
		Submitted: submitted,
	})
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm stack source")
	}

	h.audit(ctx, input.EnvironmentID, "stack.source.update", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return &handlerutil.Out[types.SwarmStackSource]{Body: types.ApiResponse[types.SwarmStackSource]{Success: true, Data: *source}}, nil
}

func (h *Handler) DeleteStack(ctx context.Context, input *DeleteSwarmStackInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.RemoveStack(ctx, input.EnvironmentID, input.Name); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to remove swarm stack")
	}

	h.audit(ctx, input.EnvironmentID, "stack.delete", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return handlerutil.MessageOutput("Swarm stack removed successfully", ""), nil
}

func (h *Handler) ListStackServices(ctx context.Context, input *ListSwarmStackServicesInput) (*handlerutil.Page[types.SwarmServiceSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListStackServicesPaginated(ctx, input.Name, params)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to list swarm stack services")
	}

	return &handlerutil.Page[types.SwarmServiceSummary]{Body: types.Paginated[types.SwarmServiceSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

func (h *Handler) ListStackTasks(ctx context.Context, input *ListSwarmStackTasksInput) (*handlerutil.Page[types.SwarmTaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListStackTasksPaginated(ctx, input.Name, params)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, h.mapError(err, "Failed to list swarm stack tasks")
	}

	return &handlerutil.Page[types.SwarmTaskSummary]{Body: types.Paginated[types.SwarmTaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

func (h *Handler) RenderStackConfig(ctx context.Context, input *RenderSwarmStackConfigInput) (*handlerutil.Out[types.SwarmStackRenderConfigResponse], error) {
	resp, err := h.service.RenderStackConfig(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to render swarm stack config")
	}

	return &handlerutil.Out[types.SwarmStackRenderConfigResponse]{Body: types.ApiResponse[types.SwarmStackRenderConfigResponse]{Success: true, Data: *resp}}, nil
}
