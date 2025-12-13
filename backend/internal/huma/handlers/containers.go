package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	containertypes "github.com/getarcaneapp/arcane/types/container"
)

type ContainerHandler struct {
	containerService *services.ContainerService
	dockerService    *services.DockerClientService
}

// Paginated response
type ContainerPaginatedResponse struct {
	Success    bool                        `json:"success"`
	Data       []containertypes.Summary    `json:"data"`
	Counts     containertypes.StatusCounts `json:"counts"`
	Pagination base.PaginationResponse     `json:"pagination"`
}

type ListContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
}

type ListContainersOutput struct {
	Body ContainerPaginatedResponse
}

type GetContainerStatusCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// ContainerStatusCountsResponse is a dedicated response type to avoid schema name collision
type ContainerStatusCountsResponse struct {
	Success bool                        `json:"success"`
	Data    containertypes.StatusCounts `json:"data"`
}

type GetContainerStatusCountsOutput struct {
	Body ContainerStatusCountsResponse
}

type CreateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          containertypes.Create
}

// ContainerCreatedResponse is a dedicated response type
type ContainerCreatedResponse struct {
	Success bool                   `json:"success"`
	Data    containertypes.Created `json:"data"`
}

type CreateContainerOutput struct {
	Body ContainerCreatedResponse
}

type GetContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

// ContainerDetailsResponse is a dedicated response type
type ContainerDetailsResponse struct {
	Success bool                   `json:"success"`
	Data    containertypes.Details `json:"data"`
}

type GetContainerOutput struct {
	Body ContainerDetailsResponse
}

type ContainerActionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

// ContainerActionResponse is a dedicated response type
type ContainerActionResponse struct {
	Success bool                 `json:"success"`
	Data    base.MessageResponse `json:"data"`
}

type ContainerActionOutput struct {
	Body ContainerActionResponse
}

type DeleteContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Force         bool   `query:"force" default:"false" doc:"Force delete running container"`
	RemoveVolumes bool   `query:"volumes" default:"false" doc:"Remove associated volumes"`
}

type DeleteContainerOutput struct {
	Body ContainerActionResponse
}

// RegisterContainers registers container endpoints.
func RegisterContainers(api huma.API, containerSvc *services.ContainerService, dockerSvc *services.DockerClientService) {
	h := &ContainerHandler{
		containerService: containerSvc,
		dockerService:    dockerSvc,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-containers",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers",
		Summary:     "List containers",
		Description: "Paginated list of containers",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListContainers)

	huma.Register(api, huma.Operation{
		OperationID: "container-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/counts",
		Summary:     "Container status counts",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetContainerStatusCounts)

	huma.Register(api, huma.Operation{
		OperationID: "create-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers",
		Summary:     "Create container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateContainer)

	huma.Register(api, huma.Operation{
		OperationID: "get-container",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Get container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetContainer)

	huma.Register(api, huma.Operation{
		OperationID: "start-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/start",
		Summary:     "Start container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.StartContainer)

	huma.Register(api, huma.Operation{
		OperationID: "stop-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/stop",
		Summary:     "Stop container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.StopContainer)

	huma.Register(api, huma.Operation{
		OperationID: "restart-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/restart",
		Summary:     "Restart container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.RestartContainer)

	huma.Register(api, huma.Operation{
		OperationID: "delete-container",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Delete container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeleteContainer)
}

func (h *ContainerHandler) ListContainers(ctx context.Context, input *ListContainersInput) (*ListContainersOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: input.Search},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
	}

	containers, paginationResp, counts, err := h.containerService.ListContainersPaginated(ctx, params, true)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerListError{Err: err}).Error())
	}

	return &ListContainersOutput{
		Body: ContainerPaginatedResponse{
			Success: true,
			Data:    containers,
			Counts:  counts,
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

func (h *ContainerHandler) GetContainerStatusCounts(ctx context.Context, input *GetContainerStatusCountsInput) (*GetContainerStatusCountsOutput, error) {
	if h.dockerService == nil {
		return nil, huma.Error500InternalServerError("docker service not available")
	}

	_, running, stopped, total, err := h.dockerService.GetAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStatusCountsError{Err: err}).Error())
	}

	return &GetContainerStatusCountsOutput{
		Body: ContainerStatusCountsResponse{
			Success: true,
			Data: containertypes.StatusCounts{
				RunningContainers: int(running),
				StoppedContainers: int(stopped),
				TotalContainers:   int(total),
			},
		},
	}, nil
}

func (h *ContainerHandler) CreateContainer(ctx context.Context, input *CreateContainerInput) (*CreateContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	// Build Docker config from input
	config := &dockercontainer.Config{
		Image:        input.Body.Image,
		Cmd:          input.Body.Command,
		Entrypoint:   input.Body.Entrypoint,
		WorkingDir:   input.Body.WorkingDir,
		User:         input.Body.User,
		Env:          input.Body.Environment,
		ExposedPorts: nat.PortSet{},
		Labels: map[string]string{
			"com.arcane.created": "true",
		},
	}

	portBindings := nat.PortMap{}
	for containerPort, hostPort := range input.Body.Ports {
		port, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			return nil, huma.Error400BadRequest((&common.InvalidPortFormatError{Err: err}).Error())
		}
		config.ExposedPorts[port] = struct{}{}
		portBindings[port] = []nat.PortBinding{{HostPort: hostPort}}
	}

	hostConfig := &dockercontainer.HostConfig{
		Binds:         input.Body.Volumes,
		PortBindings:  portBindings,
		Privileged:    input.Body.Privileged,
		AutoRemove:    input.Body.AutoRemove,
		RestartPolicy: dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyMode(input.Body.RestartPolicy)},
	}

	if input.Body.Memory > 0 {
		hostConfig.Memory = input.Body.Memory
	}
	if input.Body.CPUs > 0 {
		hostConfig.NanoCPUs = int64(input.Body.CPUs * 1e9)
	}

	var networkingConfig *network.NetworkingConfig
	if len(input.Body.Networks) > 0 {
		networkingConfig = &network.NetworkingConfig{
			EndpointsConfig: make(map[string]*network.EndpointSettings),
		}
		for _, net := range input.Body.Networks {
			networkingConfig.EndpointsConfig[net] = &network.EndpointSettings{}
		}
	}

	containerJSON, err := h.containerService.CreateContainer(ctx, config, hostConfig, networkingConfig, input.Body.Name, *user, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerCreationError{Err: err}).Error())
	}

	out := containertypes.Created{
		ID:      containerJSON.ID,
		Name:    containerJSON.Name,
		Image:   containerJSON.Config.Image,
		Status:  containerJSON.State.Status,
		Created: containerJSON.Created,
	}

	return &CreateContainerOutput{
		Body: ContainerCreatedResponse{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *ContainerHandler) GetContainer(ctx context.Context, input *GetContainerInput) (*GetContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	containerInspect, err := h.containerService.GetContainerByID(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ContainerRetrievalError{Err: err}).Error())
	}

	details := containertypes.NewDetails(containerInspect)

	return &GetContainerOutput{
		Body: ContainerDetailsResponse{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ContainerHandler) StartContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	if err := h.containerService.StartContainer(ctx, input.ContainerID, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStartError{Err: err}).Error())
	}

	return &ContainerActionOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Container started successfully"},
		},
	}, nil
}

func (h *ContainerHandler) StopContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	if err := h.containerService.StopContainer(ctx, input.ContainerID, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStopError{Err: err}).Error())
	}

	return &ContainerActionOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Container stopped successfully"},
		},
	}, nil
}

func (h *ContainerHandler) RestartContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	if err := h.containerService.RestartContainer(ctx, input.ContainerID, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerRestartError{Err: err}).Error())
	}

	return &ContainerActionOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Container restarted successfully"},
		},
	}, nil
}

func (h *ContainerHandler) DeleteContainer(ctx context.Context, input *DeleteContainerInput) (*DeleteContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	if err := h.containerService.DeleteContainer(ctx, input.ContainerID, input.Force, input.RemoveVolumes, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerDeleteError{Err: err}).Error())
	}

	return &DeleteContainerOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Container deleted successfully"},
		},
	}, nil
}
