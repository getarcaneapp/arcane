package services

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

	ListSwarmServicesInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	}

	GetSwarmServiceInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ServiceID     string `path:"serviceId" doc:"Service ID"`
	}

	CreateSwarmServiceInput struct {
		EnvironmentID string                          `path:"id" doc:"Environment ID"`
		Body          types.SwarmServiceCreateRequest `doc:"Service creation request"`
	}

	UpdateSwarmServiceInput struct {
		Body          types.SwarmServiceUpdateRequest
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ServiceID     string `path:"serviceId" doc:"Service ID"`
	}

	DeleteSwarmServiceInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ServiceID     string `path:"serviceId" doc:"Service ID"`
	}

	ListSwarmServiceTasksInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ServiceID     string `path:"serviceId" doc:"Service ID"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	}

	SwarmServiceActionInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ServiceID     string `path:"serviceId" doc:"Service ID"`
		Action        string `query:"action" required:"true" enum:"rollback" doc:"Service action"`
	}

	ScaleSwarmServiceInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ServiceID     string `path:"serviceId" doc:"Service ID"`
		Body          types.SwarmServiceScaleRequest
	}
)

func NewHandler(
	service *Service,
	audit func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any),
	mapError func(err error, fallback string) error,
) *Handler {
	return &Handler{service: service, audit: audit, mapError: mapError}
}

func (h *Handler) ListServices(ctx context.Context, input *ListSwarmServicesInput) (*handlerutil.Page[types.SwarmServiceSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list swarm services: " + err.Error())
		}
		return &handlerutil.Page[types.SwarmServiceSummary]{Body: body}, nil
	}
	items, paginationResp, err := h.service.ListServicesPaginated(ctx, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm services: "+err.Error())
	}

	return &handlerutil.Page[types.SwarmServiceSummary]{Body: types.Paginated[types.SwarmServiceSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

func (h *Handler) GetService(ctx context.Context, input *GetSwarmServiceInput) (*handlerutil.Out[types.SwarmServiceInspect], error) {
	service, err := h.service.GetService(ctx, input.ServiceID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm service not found: " + err.Error())
		}
		return nil, h.mapError(err, "Swarm service not found: "+err.Error())
	}

	return &handlerutil.Out[types.SwarmServiceInspect]{Body: types.ApiResponse[types.SwarmServiceInspect]{Success: true, Data: *service}}, nil
}

func (h *Handler) CreateService(ctx context.Context, input *CreateSwarmServiceInput) (*handlerutil.Out[types.SwarmServiceCreateResponse], error) {
	resp, err := h.service.CreateService(ctx, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to create swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.create", "swarm_service", resp.ID, "", map[string]any{"serviceId": resp.ID})

	return &handlerutil.Out[types.SwarmServiceCreateResponse]{Body: types.ApiResponse[types.SwarmServiceCreateResponse]{Success: true, Data: *resp}}, nil
}

func (h *Handler) UpdateService(ctx context.Context, input *UpdateSwarmServiceInput) (*handlerutil.Out[types.SwarmServiceUpdateResponse], error) {
	resp, err := h.service.UpdateService(ctx, input.ServiceID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.update", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[types.SwarmServiceUpdateResponse]{Body: types.ApiResponse[types.SwarmServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

func (h *Handler) DeleteService(ctx context.Context, input *DeleteSwarmServiceInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.RemoveService(ctx, input.ServiceID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm service not found: " + err.Error())
		}
		return nil, h.mapError(err, "Failed to remove swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.delete", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return handlerutil.MessageOutput("Swarm service removed successfully", ""), nil
}

func (h *Handler) ListServiceTasks(ctx context.Context, input *ListSwarmServiceTasksInput) (*handlerutil.Page[types.SwarmTaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListServiceTasksPaginated(ctx, input.ServiceID, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm tasks: "+err.Error())
	}

	return &handlerutil.Page[types.SwarmTaskSummary]{Body: types.Paginated[types.SwarmTaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

func (h *Handler) RollbackService(ctx context.Context, input *SwarmServiceActionInput) (*handlerutil.Out[types.SwarmServiceUpdateResponse], error) {
	resp, err := h.service.RollbackService(ctx, input.ServiceID)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.rollback", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[types.SwarmServiceUpdateResponse]{Body: types.ApiResponse[types.SwarmServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

func (h *Handler) ScaleService(ctx context.Context, input *ScaleSwarmServiceInput) (*handlerutil.Out[types.SwarmServiceUpdateResponse], error) {
	resp, err := h.service.ScaleService(ctx, input.ServiceID, input.Body.Replicas)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.scale", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID, "replicas": input.Body.Replicas})

	return &handlerutil.Out[types.SwarmServiceUpdateResponse]{Body: types.ApiResponse[types.SwarmServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}
