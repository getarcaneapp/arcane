package handlers

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

type ContainerHandler struct {
	containerService *services.ContainerService
	dockerService    *services.DockerClientService
	settingsService  *services.SettingsService
	activityService  *services.ActivityService
	appCtx           context.Context
}

// ContainerPaginatedResponse is the paginated list response for containers.
type ContainerPaginatedResponse struct {
	Success    bool                          `json:"success"`
	Data       []containertypes.Summary      `json:"data"`
	Groups     []containertypes.SummaryGroup `json:"groups,omitempty"`
	Counts     containertypes.StatusCounts   `json:"counts"`
	Pagination base.PaginationResponse       `json:"pagination"`
}

type ListContainersInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	Search          string `query:"search" doc:"Search query"`
	Sort            string `query:"sort" doc:"Column to sort by"`
	Order           string `query:"order" default:"asc" doc:"Sort direction"`
	Start           int    `query:"start" default:"0" doc:"Start index"`
	Limit           int    `query:"limit" default:"20" doc:"Limit"`
	GroupBy         string `query:"groupBy" doc:"Optional grouping mode (for example: project)"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal containers"`
	Updates         string `query:"updates" doc:"Filter by update status (has_update, up_to_date, error, unknown)"`
	Standalone      string `query:"standalone" doc:"Filter standalone containers only (true/false)"`
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

// SetAutoUpdateInput is the request input for toggling container auto-update.
type SetAutoUpdateInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          struct {
		Enabled bool `json:"enabled" doc:"Whether auto-update is enabled for this container"`
	}
}

type SetAutoUpdateOutput struct {
	Body ContainerActionResponse
}

// KillContainerInput carries the optional signal for a container kill.
type KillContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Signal        string `query:"signal" doc:"Signal to send (for example SIGTERM, SIGKILL). Defaults to SIGKILL."`
}

type CommitContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          containertypes.CommitRequest
}

type CommitContainerOutput struct {
	Body base.ApiResponse[containertypes.CommitResult]
}

func RegisterContainers(api huma.API, containerSvc *services.ContainerService, dockerSvc *services.DockerClientService, settingsSvc *services.SettingsService, activitySvc *services.ActivityService, appCtx ActivityAppContext) {
	h := &ContainerHandler{
		containerService: containerSvc,
		dockerService:    dockerSvc,
		settingsService:  settingsSvc,
		activityService:  activitySvc,
		appCtx:           appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-containers",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers",
		Summary:     "List containers",
		Description: "Paginated list of containers",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersList, h.ListContainers)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "container-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/counts",
		Summary:     "Container status counts",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersList, h.GetContainerStatusCounts)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers",
		Summary:     "Create container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersCreate, h.CreateContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-container",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Get container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersRead, h.GetContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "start-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/start",
		Summary:     "Start container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersStart, h.StartContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "stop-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/stop",
		Summary:     "Stop container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersStop, h.StopContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "restart-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/restart",
		Summary:     "Restart container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersRestart, h.RestartContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "kill-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/kill",
		Summary:     "Kill container",
		Description: "Send a signal to the container's main process (default SIGKILL)",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersKill, h.KillContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "pause-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/pause",
		Summary:     "Pause container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersPause, h.PauseContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "unpause-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/unpause",
		Summary:     "Unpause container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersPause, h.UnpauseContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "commit-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/commit",
		Summary:     "Commit container",
		Description: "Create an image from a container",
		Tags:        []string{"Containers", "Images"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermImagesCommit, h.CommitContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "redeploy-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/redeploy",
		Summary:     "Redeploy container",
		Description: "Pull latest image and recreate container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersRedeploy, h.RedeployContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-container",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Delete container",
		Tags:        []string{"Containers"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersDelete, h.DeleteContainer)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "set-container-auto-update",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/containers/{containerId}/auto-update",
		Summary:     "Set container auto-update",
		Description: "Enable or disable auto-update for a specific container",
		Tags:        []string{"Containers", "Updater"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermContainersAutoUpdate, h.SetAutoUpdate)
}

func (h *ContainerHandler) ListContainers(ctx context.Context, input *ListContainersInput) (*ListContainersOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}
	if input.Standalone != "" {
		params.Filters["standalone"] = input.Standalone
	}

	result, err := h.containerService.ListContainersPaginated(ctx, params, true, input.IncludeInternal, input.GroupBy)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerListError{Err: err}).Error())
	}

	return &ListContainersOutput{
		Body: ContainerPaginatedResponse{
			Success:    true,
			Data:       result.Items,
			Groups:     result.Groups,
			Counts:     result.Counts,
			Pagination: toPaginationResponseInternal(result.Pagination),
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
				RunningContainers: running,
				StoppedContainers: stopped,
				TotalContainers:   total,
			},
		},
	}, nil
}

func parsePortSpec(spec string) (network.Port, error) {
	if strings.Contains(spec, "/") {
		return network.ParsePort(spec)
	}

	return network.ParsePort(spec + "/tcp")
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
		ExposedPorts:    network.PortSet{},
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

func applyLegacyPortBindings(body containertypes.Create, config *dockercontainer.Config, portBindings network.PortMap) error {
	for containerPort, hostPort := range body.Ports {
		port, err := network.ParsePort(containerPort + "/tcp")
		if err != nil {
			return err
		}
		config.ExposedPorts[port] = struct{}{}
		portBindings[port] = []network.PortBinding{{HostPort: hostPort}}
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

func buildHostConfigBase(body containertypes.Create, portBindings network.PortMap) *dockercontainer.HostConfig {
	return &dockercontainer.HostConfig{
		Binds:         body.Volumes,
		PortBindings:  portBindings,
		Privileged:    body.Privileged,
		AutoRemove:    body.AutoRemove,
		RestartPolicy: dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyMode(body.RestartPolicy)},
	}
}

func applyHostConfigPortBindings(config *dockercontainer.Config, portBindings network.PortMap, bindings map[string][]containertypes.PortBindingCreate) error {
	for portSpec, bindingList := range bindings {
		port, err := parsePortSpec(portSpec)
		if err != nil {
			return err
		}
		config.ExposedPorts[port] = struct{}{}
		for _, binding := range bindingList {
			pb := network.PortBinding{HostPort: binding.HostPort}
			if hostIP := strings.TrimSpace(binding.HostIP); hostIP != "" {
				parsedIP, err := netip.ParseAddr(hostIP)
				if err != nil {
					return err
				}
				pb.HostIP = parsedIP
			}
			portBindings[port] = append(portBindings[port], pb)
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

func applyHostConfigOverrides(body containertypes.Create, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, portBindings network.PortMap) error {
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

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	config := buildContainerConfig(input.Body)
	portBindings := network.PortMap{}
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
		Status:  string(containerJSON.State.Status),
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

	details, err := h.containerService.GetContainerDetails(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ContainerRetrievalError{Err: err}).Error())
	}

	return &GetContainerOutput{
		Body: ContainerDetailsResponse{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ContainerHandler) StartContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    models.ActivityTypeContainerStart,
		Step:            "Starting container",
		StartMessage:    "Container start requested",
		CompleteMessage: "Container started",
		SuccessMessage:  "Container started successfully",
		Action: func(runtimeCtx context.Context, containerID string, user models.User) error {
			return h.containerService.StartContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError((&common.ContainerStartError{Err: err}).Error())
		},
	})
}

func (h *ContainerHandler) StopContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    models.ActivityTypeContainerStop,
		Step:            "Stopping container",
		StartMessage:    "Container stop requested",
		CompleteMessage: "Container stopped",
		SuccessMessage:  "Container stopped successfully",
		Action: func(runtimeCtx context.Context, containerID string, user models.User) error {
			return h.containerService.StopContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError((&common.ContainerStopError{Err: err}).Error())
		},
	})
}

func (h *ContainerHandler) RestartContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    models.ActivityTypeContainerRestart,
		Step:            "Restarting container",
		StartMessage:    "Container restart requested",
		CompleteMessage: "Container restarted",
		SuccessMessage:  "Container restarted successfully",
		Action: func(runtimeCtx context.Context, containerID string, user models.User) error {
			return h.containerService.RestartContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError((&common.ContainerRestartError{Err: err}).Error())
		},
	})
}

func (h *ContainerHandler) KillContainer(ctx context.Context, input *KillContainerInput) (*ContainerActionOutput, error) {
	signal := strings.TrimSpace(input.Signal)
	return h.runContainerActionInternal(ctx, &ContainerActionInput{EnvironmentID: input.EnvironmentID, ContainerID: input.ContainerID}, containerActionConfigInternal{
		ActivityType:    models.ActivityTypeContainerKill,
		Step:            "Killing container",
		StartMessage:    "Container kill requested",
		CompleteMessage: "Container killed",
		SuccessMessage:  "Container killed successfully",
		Action: func(runtimeCtx context.Context, containerID string, user models.User) error {
			return h.containerService.KillContainer(runtimeCtx, containerID, signal, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError((&common.ContainerKillError{Err: err}).Error())
		},
	})
}

func (h *ContainerHandler) PauseContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    models.ActivityTypeContainerPause,
		Step:            "Pausing container",
		StartMessage:    "Container pause requested",
		CompleteMessage: "Container paused",
		SuccessMessage:  "Container paused successfully",
		Action: func(runtimeCtx context.Context, containerID string, user models.User) error {
			return h.containerService.PauseContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError((&common.ContainerPauseError{Err: err}).Error())
		},
	})
}

func (h *ContainerHandler) UnpauseContainer(ctx context.Context, input *ContainerActionInput) (*ContainerActionOutput, error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    models.ActivityTypeContainerUnpause,
		Step:            "Unpausing container",
		StartMessage:    "Container unpause requested",
		CompleteMessage: "Container unpaused",
		SuccessMessage:  "Container unpaused successfully",
		Action: func(runtimeCtx context.Context, containerID string, user models.User) error {
			return h.containerService.UnpauseContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError((&common.ContainerUnpauseError{Err: err}).Error())
		},
	})
}

type containerActionConfigInternal struct {
	ActivityType    models.ActivityType
	Step            string
	StartMessage    string
	CompleteMessage string
	SuccessMessage  string
	Action          func(context.Context, string, models.User) error
	Error           func(error) error
}

func (h *ContainerHandler) runContainerActionInternal(ctx context.Context, input *ContainerActionInput, cfg containerActionConfigInternal) (*ContainerActionOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(runtimeCtx, h.activityService, input.EnvironmentID, cfg.ActivityType, "container", input.ContainerID, input.ContainerID, user, cfg.Step, cfg.StartMessage, models.JSON{"containerID": input.ContainerID})
	if err := cfg.Action(runtimeCtx, input.ContainerID, *user); err != nil {
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, err)
		return nil, cfg.Error(err)
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, nil)

	return &ContainerActionOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: cfg.SuccessMessage, ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}

func (h *ContainerHandler) CommitContainer(ctx context.Context, input *CommitContainerInput) (*CommitContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if strings.TrimSpace(input.ContainerID) == "" {
		return nil, huma.Error400BadRequest("container ID is required")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	out, err := h.containerService.CommitContainer(ctx, input.ContainerID, input.Body, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to commit container: %v", err))
	}

	return &CommitContainerOutput{
		Body: base.ApiResponse[containertypes.CommitResult]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

func (h *ContainerHandler) RedeployContainer(ctx context.Context, input *ContainerActionInput) (*GetContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(runtimeCtx, h.activityService, input.EnvironmentID, models.ActivityTypeContainerRedeploy, "container", input.ContainerID, input.ContainerID, user, "Starting redeploy", "Container redeploy requested", models.JSON{"containerID": input.ContainerID})
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Redeploying container")
	redeployCtx := context.WithValue(runtimeCtx, projects.ProgressWriterKey{}, activityWriter)
	newContainerID, err := h.containerService.RedeployContainer(redeployCtx, input.ContainerID, *user)
	if err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeploy failed", err)
		return nil, huma.Error500InternalServerError((&common.ContainerRedeployError{Err: err}).Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeployed", nil)

	// Fetch full container details to return (consistent with other endpoints)
	details, inspectErr := h.containerService.GetContainerDetails(runtimeCtx, newContainerID)
	if inspectErr == nil {
		details.ActivityID = utils.StringPtrFromTrimmed(activityID)

		return &GetContainerOutput{
			Body: ContainerDetailsResponse{
				Success: true,
				Data:    details,
			},
		}, nil
	}

	// Container was redeployed successfully, but we couldn't fetch full details.
	// Return minimal response with just the ID so frontend can still navigate.
	return &GetContainerOutput{
		Body: ContainerDetailsResponse{
			Success: true,
			Data: containertypes.Details{
				ID:         newContainerID,
				ActivityID: utils.StringPtrFromTrimmed(activityID),
			},
		},
	}, nil
}

func (h *ContainerHandler) DeleteContainer(ctx context.Context, input *DeleteContainerInput) (*DeleteContainerOutput, error) {
	if h.containerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(runtimeCtx, h.activityService, input.EnvironmentID, models.ActivityTypeContainerDelete, "container", input.ContainerID, input.ContainerID, user, "Deleting container", "Container delete requested", models.JSON{"containerID": input.ContainerID, "force": input.Force, "removeVolumes": input.RemoveVolumes})
	if err := h.containerService.DeleteContainer(runtimeCtx, input.ContainerID, input.Force, input.RemoveVolumes, *user); err != nil {
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container deleted", err)
		return nil, huma.Error500InternalServerError((&common.ContainerDeleteError{Err: err}).Error())
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container deleted", nil)

	return &DeleteContainerOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Container deleted successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}

func (h *ContainerHandler) SetAutoUpdate(ctx context.Context, input *SetAutoUpdateInput) (*SetAutoUpdateOutput, error) {
	if h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// Resolve container name from ID
	containerName, err := h.containerService.GetContainerNameByID(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound("container not found")
	}

	excluded := !input.Body.Enabled
	if err := h.settingsService.SetContainerAutoUpdateExclusionInternal(ctx, containerName, excluded); err != nil {
		return nil, huma.Error500InternalServerError("failed to update auto-update setting")
	}

	msg := "Auto-update enabled"
	if excluded {
		msg = "Auto-update disabled"
	}

	return &SetAutoUpdateOutput{
		Body: ContainerActionResponse{
			Success: true,
			Data:    base.MessageResponse{Message: msg},
		},
	}, nil
}
