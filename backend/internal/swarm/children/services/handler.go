package services

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/swarm"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the swarm service endpoints.
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

type ListSwarmServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type CreateSwarmServiceInput struct {
	EnvironmentID string                     `path:"id" doc:"Environment ID"`
	Body          swarm.ServiceCreateRequest `doc:"Service creation request"`
}

type UpdateSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarm.ServiceUpdateRequest
}

type DeleteSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type ListSwarmServiceTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type RollbackSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type ScaleSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarm.ServiceScaleRequest
}

// ListServices lists swarm services for an environment and returns a paginated response.
//
// It normalizes the search, sort, and pagination fields from input, delegates
// the lookup to the swarm service, and returns an empty slice instead of nil
// when no services are found.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies the environment ID plus optional search, sorting, and pagination values.
//
// Returns a successful response containing service summaries and pagination metadata.
// Returns an HTTP-shaped error if the swarm service is unavailable or if the
// underlying swarm lookup fails.
func (h *Handler) ListServices(ctx context.Context, input *ListSwarmServicesInput) (*handlerutil.Page[swarm.ServiceSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListServicesPaginated(ctx, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm services: "+err.Error())
	}
	if items == nil {
		items = []swarm.ServiceSummary{}
	}

	return &handlerutil.Page[swarm.ServiceSummary]{Body: base.Paginated[swarm.ServiceSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// GetService returns detailed information for a single swarm service.
//
// It loads the service by ID through the swarm service and converts lookup
// failures into the HTTP errors expected by the API.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and the swarm service to inspect.
//
// Returns a successful response containing the service inspection payload.
// Returns `404 Not Found` when the service does not exist and other mapped HTTP
// errors when the inspection fails.
func (h *Handler) GetService(ctx context.Context, input *GetSwarmServiceInput) (*handlerutil.Out[swarm.ServiceInspect], error) {
	service, err := h.service.GetService(ctx, input.ServiceID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm service not found: " + err.Error())
		}
		return nil, h.mapError(err, "Swarm service not found: "+err.Error())
	}

	return &handlerutil.Out[swarm.ServiceInspect]{Body: base.ApiResponse[swarm.ServiceInspect]{Success: true, Data: *service}}, nil
}

// CreateService creates a new swarm service in the target environment.
//
// It requires admin privileges, forwards the create request to the swarm
// service, and records an audit event after a successful mutation.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input contains the environment ID and the requested service specification.
//
// Returns a successful response containing the created service ID and any Docker warnings.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when validation or creation fails.
func (h *Handler) CreateService(ctx context.Context, input *CreateSwarmServiceInput) (*handlerutil.Out[swarm.ServiceCreateResponse], error) {
	resp, err := h.service.CreateService(ctx, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to create swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.create", "swarm_service", resp.ID, "", map[string]any{"serviceId": resp.ID})

	return &handlerutil.Out[swarm.ServiceCreateResponse]{Body: base.ApiResponse[swarm.ServiceCreateResponse]{Success: true, Data: *resp}}, nil
}

// UpdateService updates an existing swarm service.
//
// It requires admin privileges, submits the requested versioned update to the
// swarm service, and emits an audit event when the update succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the service to update and provides the replacement specification and options.
//
// Returns a successful response containing any Docker warnings.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the update request is invalid or the underlying update fails.
func (h *Handler) UpdateService(ctx context.Context, input *UpdateSwarmServiceInput) (*handlerutil.Out[swarm.ServiceUpdateResponse], error) {
	resp, err := h.service.UpdateService(ctx, input.ServiceID, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.update", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[swarm.ServiceUpdateResponse]{Body: base.ApiResponse[swarm.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

// DeleteService removes a swarm service.
//
// It requires admin privileges, asks the swarm service to remove the service,
// translates missing-service conditions to `404 Not Found`, and records an
// audit event after removal.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and service to remove.
//
// Returns a successful response with a confirmation message.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the service does not exist, or another mapped HTTP error when removal fails.
func (h *Handler) DeleteService(ctx context.Context, input *DeleteSwarmServiceInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.RemoveService(ctx, input.ServiceID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm service not found: " + err.Error())
		}
		return nil, h.mapError(err, "Failed to remove swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.delete", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return handlerutil.MessageOutput("Swarm service removed successfully", ""), nil
}

// ListServiceTasks lists tasks belonging to a specific swarm service.
//
// It applies the requested search, sort, and pagination values, delegates the
// lookup to the swarm service, and normalizes nil task slices to empty arrays.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the service and supplies optional filtering and pagination fields.
//
// Returns a paginated list of task summaries for the service.
// Returns a mapped HTTP error when the swarm task lookup fails.
func (h *Handler) ListServiceTasks(ctx context.Context, input *ListSwarmServiceTasksInput) (*handlerutil.Page[swarm.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.service.ListServiceTasksPaginated(ctx, input.ServiceID, params)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm tasks: "+err.Error())
	}
	if items == nil {
		items = []swarm.TaskSummary{}
	}

	return &handlerutil.Page[swarm.TaskSummary]{Body: base.Paginated[swarm.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// RollbackService requests a server-side rollback for a swarm service.
//
// It requires admin privileges, delegates the rollback to the swarm service,
// and records an audit event describing the mutation.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and service to roll back.
//
// Returns a successful response containing any warnings reported by Docker.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the rollback cannot be performed.
func (h *Handler) RollbackService(ctx context.Context, input *RollbackSwarmServiceInput) (*handlerutil.Out[swarm.ServiceUpdateResponse], error) {
	resp, err := h.service.RollbackService(ctx, input.ServiceID)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.rollback", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[swarm.ServiceUpdateResponse]{Body: base.ApiResponse[swarm.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

// ScaleService changes the replica count of a swarm service.
//
// It requires admin privileges, forwards the requested replica count to the
// swarm service, and records the new replica target in the audit metadata.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the service and supplies the desired replica count.
//
// Returns a successful response containing any warnings reported by Docker.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when scaling is invalid or the update fails.
func (h *Handler) ScaleService(ctx context.Context, input *ScaleSwarmServiceInput) (*handlerutil.Out[swarm.ServiceUpdateResponse], error) {
	resp, err := h.service.ScaleService(ctx, input.ServiceID, input.Body.Replicas)
	if err != nil {
		return nil, h.mapError(err, "Failed to update swarm service: "+err.Error())
	}

	h.audit(ctx, input.EnvironmentID, "service.scale", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID, "replicas": input.Body.Replicas})

	return &handlerutil.Out[swarm.ServiceUpdateResponse]{Body: base.ApiResponse[swarm.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}
