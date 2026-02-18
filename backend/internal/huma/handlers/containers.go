package handlers

import (
	"context"
	"maps"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
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
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	Search          string `query:"search" doc:"Search query"`
	Sort            string `query:"sort" doc:"Column to sort by"`
	Order           string `query:"order" default:"asc" doc:"Sort direction"`
	Start           int    `query:"start" default:"0" doc:"Start index"`
	Limit           int    `query:"limit" default:"20" doc:"Limit"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal containers"`
	Updates         string `query:"updates" doc:"Filter by update status (has_update, up_to_date, error, unknown)"`
}

type ListContainersOutput struct {
	Body ContainerPaginatedResponse
}

type GetContainerStatusCountsInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal containers"`
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

	filters := make(map[string]string)
	if input.Updates != "" {
		filters["updates"] = input.Updates
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
		Filters: filters,
	}

	containers, paginationResp, counts, err := h.containerService.ListContainersPaginated(ctx, params, true, input.IncludeInternal)
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

	containers, _, _, _, err := h.dockerService.GetAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStatusCountsError{Err: err}).Error())
	}

	if !input.IncludeInternal {
		filtered := make([]dockercontainer.Summary, 0, len(containers))
		for _, c := range containers {
			if libarcane.IsInternalContainer(c.Labels) {
				continue
			}
			filtered = append(filtered, c)
		}
		containers = filtered
	}

	running, stopped := 0, 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}
	total := len(containers)

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

func parsePortSpec(spec string) (nat.Port, error) {
	proto := "tcp"
	port := spec
	if strings.Contains(spec, "/") {
		parts := strings.SplitN(spec, "/", 2)
		port = parts[0]
		if parts[1] != "" {
			proto = parts[1]
		}
	}

	return nat.NewPort(proto, port)
}

func resolveCreateCommand(body containertypes.Create) []string {
	if len(body.Command) > 0 {
		return body.Command
	}

	return body.Cmd
}

func resolveCreateEnv(body containertypes.Create) []string {
	if len(body.Environment) > 0 {
		return body.Environment
	}

	return body.Env
}

func buildCreateLabels(body containertypes.Create) map[string]string {
	labels := map[string]string{
		"com.arcane.created": "true",
	}
	maps.Copy(labels, body.Labels)

	return labels
}

func buildContainerConfig(body containertypes.Create) *dockercontainer.Config {
	return &dockercontainer.Config{
		Image:           body.Image,
		Cmd:             resolveCreateCommand(body),
		Entrypoint:      body.Entrypoint,
		WorkingDir:      body.WorkingDir,
		User:            body.User,
		Env:             resolveCreateEnv(body),
		ExposedPorts:    nat.PortSet{},
		Labels:          buildCreateLabels(body),
		Hostname:        body.Hostname,
		Domainname:      body.Domainname,
		AttachStdout:    body.AttachStdout,
		AttachStderr:    body.AttachStderr,
		AttachStdin:     body.AttachStdin,
		Tty:             body.Tty,
		OpenStdin:       body.OpenStdin,
		StdinOnce:       body.StdinOnce,
		NetworkDisabled: body.NetworkDisabled,
	}
}

func applyLegacyPortBindings(body containertypes.Create, config *dockercontainer.Config, portBindings nat.PortMap) error {
	for containerPort, hostPort := range body.Ports {
		port, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			return err
		}
		config.ExposedPorts[port] = struct{}{}
		portBindings[port] = []nat.PortBinding{{HostPort: hostPort}}
	}

	return nil
}

func applyExposedPorts(exposedPorts map[string]struct{}, config *dockercontainer.Config) error {
	for portSpec := range exposedPorts {
		port, err := parsePortSpec(portSpec)
		if err != nil {
			return err
		}
		config.ExposedPorts[port] = struct{}{}
	}

	return nil
}

func buildHostConfigBase(body containertypes.Create, portBindings nat.PortMap) *dockercontainer.HostConfig {
	return &dockercontainer.HostConfig{
		Binds:         body.Volumes,
		PortBindings:  portBindings,
		Privileged:    body.Privileged,
		AutoRemove:    body.AutoRemove,
		RestartPolicy: dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyMode(body.RestartPolicy)},
	}
}

func applyHostConfigPortBindings(config *dockercontainer.Config, portBindings nat.PortMap, bindings map[string][]containertypes.PortBindingCreate) error {
	for portSpec, bindingList := range bindings {
		port, err := parsePortSpec(portSpec)
		if err != nil {
			return err
		}
		config.ExposedPorts[port] = struct{}{}
		for _, binding := range bindingList {
			portBindings[port] = append(portBindings[port], nat.PortBinding{
				HostPort: binding.HostPort,
				HostIP:   binding.HostIP,
			})
		}
	}

	return nil
}

func applyHostConfigSettings(hostConfig *dockercontainer.HostConfig, input *containertypes.HostConfigCreate) {
	if input == nil {
		return
	}

	if input.NetworkMode != "" {
		hostConfig.NetworkMode = dockercontainer.NetworkMode(input.NetworkMode)
	}
	if input.Privileged != nil {
		hostConfig.Privileged = *input.Privileged
	}
	if input.AutoRemove != nil {
		hostConfig.AutoRemove = *input.AutoRemove
	}
	if input.ReadonlyRootfs != nil {
		hostConfig.ReadonlyRootfs = *input.ReadonlyRootfs
	}
	if input.PublishAllPorts != nil {
		hostConfig.PublishAllPorts = *input.PublishAllPorts
	}
	if input.RestartPolicy != nil {
		hostConfig.RestartPolicy = dockercontainer.RestartPolicy{
			Name:              dockercontainer.RestartPolicyMode(input.RestartPolicy.Name),
			MaximumRetryCount: input.RestartPolicy.MaximumRetryCount,
		}
	}
	if input.Memory > 0 {
		hostConfig.Memory = input.Memory
	}
	if input.MemorySwap > 0 {
		hostConfig.MemorySwap = input.MemorySwap
	}
	if input.NanoCPUs > 0 {
		hostConfig.NanoCPUs = input.NanoCPUs
	}
	if input.CPUShares > 0 {
		hostConfig.CPUShares = input.CPUShares
	}
}

func applyHostConfigOverrides(body containertypes.Create, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, portBindings nat.PortMap) error {
	if body.HostConfig == nil {
		return nil
	}

	if len(body.HostConfig.Binds) > 0 {
		hostConfig.Binds = body.HostConfig.Binds
	}

	if len(body.HostConfig.PortBindings) > 0 {
		if err := applyHostConfigPortBindings(config, portBindings, body.HostConfig.PortBindings); err != nil {
			return err
		}
	}

	applyHostConfigSettings(hostConfig, body.HostConfig)
	return nil
}

func applyLegacyResourceLimits(body containertypes.Create, hostConfig *dockercontainer.HostConfig) {
	if body.Memory > 0 {
		hostConfig.Memory = body.Memory
	}
	if body.CPUs > 0 {
		hostConfig.NanoCPUs = int64(body.CPUs * 1e9)
	}
}

func buildNetworkingConfig(body containertypes.Create) *network.NetworkingConfig {
	if body.NetworkingConfig != nil && len(body.NetworkingConfig.EndpointsConfig) > 0 {
		networkingConfig := &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
		for name, endpoint := range body.NetworkingConfig.EndpointsConfig {
			networkingConfig.EndpointsConfig[name] = &network.EndpointSettings{Aliases: endpoint.Aliases}
		}
		return networkingConfig
	}

	if len(body.Networks) > 0 {
		networkingConfig := &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
		for _, net := range body.Networks {
			networkingConfig.EndpointsConfig[net] = &network.EndpointSettings{}
		}
		return networkingConfig
	}

	return nil
}

func (h *ContainerHandler) CreateContainer(ctx context.Context, input *CreateContainerInput) (*CreateContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	config := buildContainerConfig(input.Body)
	portBindings := nat.PortMap{}
	if err := applyLegacyPortBindings(input.Body, config, portBindings); err != nil {
		return nil, huma.Error400BadRequest((&common.InvalidPortFormatError{Err: err}).Error())
	}
	if err := applyExposedPorts(input.Body.ExposedPorts, config); err != nil {
		return nil, huma.Error400BadRequest((&common.InvalidPortFormatError{Err: err}).Error())
	}

	hostConfig := buildHostConfigBase(input.Body, portBindings)
	if err := applyHostConfigOverrides(input.Body, config, hostConfig, portBindings); err != nil {
		return nil, huma.Error400BadRequest((&common.InvalidPortFormatError{Err: err}).Error())
	}
	applyLegacyResourceLimits(input.Body, hostConfig)

	networkingConfig := buildNetworkingConfig(input.Body)

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
