package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
)

type SwarmHandler struct {
	swarmService *services.SwarmService
}

type SwarmPaginatedResponse[T any] struct {
	Success    bool                    `json:"success"`
	Data       []T                     `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListSwarmServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmServicesOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.ServiceSummary]
}

type GetSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type GetSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceInspect]
}

type CreateSwarmServiceInput struct {
	EnvironmentID string                          `path:"id" doc:"Environment ID"`
	Body          swarmtypes.ServiceCreateRequest `doc:"Service creation request"`
}

type CreateSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceCreateResponse]
}

type UpdateSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarmtypes.ServiceUpdateRequest
}

type UpdateSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceUpdateResponse]
}

type DeleteSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type DeleteSwarmServiceOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type ListSwarmNodesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmNodesOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.NodeSummary]
}

type GetSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type GetSwarmNodeOutput struct {
	Body base.ApiResponse[swarmtypes.NodeSummary]
}

type ListSwarmTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmTasksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.TaskSummary]
}

type ListSwarmStacksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmStacksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.StackSummary]
}

type DeploySwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.StackDeployRequest
}

type DeploySwarmStackOutput struct {
	Body base.ApiResponse[swarmtypes.StackDeployResponse]
}

type GetSwarmInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmInfoOutput struct {
	Body base.ApiResponse[swarmtypes.SwarmInfo]
}

func RegisterSwarm(api huma.API, swarmSvc *services.SwarmService) {
	h := &SwarmHandler{swarmService: swarmSvc}

	huma.Register(api, huma.Operation{
		OperationID: "list-swarm-services",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/services",
		Summary:     "List swarm services",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListServices)

	huma.Register(api, huma.Operation{
		OperationID: "get-swarm-service",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/services/{serviceId}",
		Summary:     "Get swarm service",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetService)

	huma.Register(api, huma.Operation{
		OperationID: "create-swarm-service",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/swarm/services",
		Summary:     "Create swarm service",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateService)

	huma.Register(api, huma.Operation{
		OperationID: "update-swarm-service",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/swarm/services/{serviceId}",
		Summary:     "Update swarm service",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.UpdateService)

	huma.Register(api, huma.Operation{
		OperationID: "delete-swarm-service",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/swarm/services/{serviceId}",
		Summary:     "Delete swarm service",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeleteService)

	huma.Register(api, huma.Operation{
		OperationID: "list-swarm-nodes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/nodes",
		Summary:     "List swarm nodes",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListNodes)

	huma.Register(api, huma.Operation{
		OperationID: "get-swarm-node",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/nodes/{nodeId}",
		Summary:     "Get swarm node",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetNode)

	huma.Register(api, huma.Operation{
		OperationID: "list-swarm-tasks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/tasks",
		Summary:     "List swarm tasks",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListTasks)

	huma.Register(api, huma.Operation{
		OperationID: "list-swarm-stacks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/stacks",
		Summary:     "List swarm stacks",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListStacks)

	huma.Register(api, huma.Operation{
		OperationID: "deploy-swarm-stack",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/swarm/stacks",
		Summary:     "Deploy swarm stack",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeployStack)

	huma.Register(api, huma.Operation{
		OperationID: "get-swarm-info",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/swarm/info",
		Summary:     "Get swarm info",
		Tags:        []string{"Swarm"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetSwarmInfo)
}

func (h *SwarmHandler) ListServices(ctx context.Context, input *ListSwarmServicesInput) (*ListSwarmServicesOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)

	services, paginationResp, err := h.swarmService.ListServicesPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceListError{Err: err}).Error())
	}

	if services == nil {
		services = []swarmtypes.ServiceSummary{}
	}

	return &ListSwarmServicesOutput{
		Body: SwarmPaginatedResponse[swarmtypes.ServiceSummary]{
			Success: true,
			Data:    services,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

func (h *SwarmHandler) GetService(ctx context.Context, input *GetSwarmServiceInput) (*GetSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	service, err := h.swarmService.GetService(ctx, input.ServiceID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmServiceNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceNotFoundError{Err: err}).Error())
	}

	return &GetSwarmServiceOutput{
		Body: base.ApiResponse[swarmtypes.ServiceInspect]{
			Success: true,
			Data:    *service,
		},
	}, nil
}

func (h *SwarmHandler) CreateService(ctx context.Context, input *CreateSwarmServiceInput) (*CreateSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	resp, err := h.swarmService.CreateService(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceCreateError{Err: err}).Error())
	}

	return &CreateSwarmServiceOutput{
		Body: base.ApiResponse[swarmtypes.ServiceCreateResponse]{
			Success: true,
			Data:    *resp,
		},
	}, nil
}

func (h *SwarmHandler) UpdateService(ctx context.Context, input *UpdateSwarmServiceInput) (*UpdateSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	resp, err := h.swarmService.UpdateService(ctx, input.ServiceID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceUpdateError{Err: err}).Error())
	}

	return &UpdateSwarmServiceOutput{
		Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{
			Success: true,
			Data:    *resp,
		},
	}, nil
}

func (h *SwarmHandler) DeleteService(ctx context.Context, input *DeleteSwarmServiceInput) (*DeleteSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.swarmService.RemoveService(ctx, input.ServiceID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmServiceNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceRemoveError{Err: err}).Error())
	}

	return &DeleteSwarmServiceOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Swarm service removed successfully",
			},
		},
	}, nil
}

func (h *SwarmHandler) ListNodes(ctx context.Context, input *ListSwarmNodesInput) (*ListSwarmNodesOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)

	nodes, paginationResp, err := h.swarmService.ListNodesPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeListError{Err: err}).Error())
	}

	if nodes == nil {
		nodes = []swarmtypes.NodeSummary{}
	}

	return &ListSwarmNodesOutput{
		Body: SwarmPaginatedResponse[swarmtypes.NodeSummary]{
			Success: true,
			Data:    nodes,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

func (h *SwarmHandler) GetNode(ctx context.Context, input *GetSwarmNodeInput) (*GetSwarmNodeOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	node, err := h.swarmService.GetNode(ctx, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmNodeNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	return &GetSwarmNodeOutput{
		Body: base.ApiResponse[swarmtypes.NodeSummary]{
			Success: true,
			Data:    *node,
		},
	}, nil
}

func (h *SwarmHandler) ListTasks(ctx context.Context, input *ListSwarmTasksInput) (*ListSwarmTasksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)

	tasks, paginationResp, err := h.swarmService.ListTasksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmTaskListError{Err: err}).Error())
	}

	if tasks == nil {
		tasks = []swarmtypes.TaskSummary{}
	}

	return &ListSwarmTasksOutput{
		Body: SwarmPaginatedResponse[swarmtypes.TaskSummary]{
			Success: true,
			Data:    tasks,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

func (h *SwarmHandler) ListStacks(ctx context.Context, input *ListSwarmStacksInput) (*ListSwarmStacksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)

	stacks, paginationResp, err := h.swarmService.ListStacksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmStackListError{Err: err}).Error())
	}

	if stacks == nil {
		stacks = []swarmtypes.StackSummary{}
	}

	return &ListSwarmStacksOutput{
		Body: SwarmPaginatedResponse[swarmtypes.StackSummary]{
			Success: true,
			Data:    stacks,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

func (h *SwarmHandler) DeployStack(ctx context.Context, input *DeploySwarmStackInput) (*DeploySwarmStackOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	resp, err := h.swarmService.DeployStack(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmStackDeployError{Err: err}).Error())
	}

	return &DeploySwarmStackOutput{
		Body: base.ApiResponse[swarmtypes.StackDeployResponse]{
			Success: true,
			Data:    *resp,
		},
	}, nil
}

func (h *SwarmHandler) GetSwarmInfo(ctx context.Context, input *GetSwarmInfoInput) (*GetSwarmInfoOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	info, err := h.swarmService.GetSwarmInfo(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmInspectError{Err: err}).Error())
	}

	return &GetSwarmInfoOutput{
		Body: base.ApiResponse[swarmtypes.SwarmInfo]{
			Success: true,
			Data:    *info,
		},
	}, nil
}

func buildSwarmQueryParams(search, sort, order string, start, limit int) pagination.QueryParams {
	if limit == 0 {
		limit = 20
	}

	return pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: strings.TrimSpace(search),
		},
		SortParams: pagination.SortParams{
			Sort:  strings.TrimSpace(sort),
			Order: pagination.SortOrder(order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: start,
			Limit: limit,
		},
	}
}

func mapSwarmServiceError(err error, fallback string) error {
	if errors.Is(err, services.ErrSwarmNotEnabled) {
		return huma.Error409Conflict((&common.SwarmNotEnabledError{}).Error())
	}
	if errors.Is(err, services.ErrSwarmManagerRequired) {
		return huma.Error403Forbidden((&common.SwarmManagerRequiredError{}).Error())
	}
	return huma.Error500InternalServerError(fallback)
}
