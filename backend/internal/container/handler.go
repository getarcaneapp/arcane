package container

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"strings"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/samber/mo"
)

type ContainerHandler struct {
	containerService *ContainerService
	dockerService    *docker.DockerClientService
	settingsService  *settings.SettingsService
	activityService  *activity.ActivityService
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

type CreateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          containertypes.Create
}

type GetContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

type ContainerActionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

type DeleteContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Force         bool   `query:"force" default:"false" doc:"Force delete running container"`
	RemoveVolumes bool   `query:"volumes" default:"false" doc:"Remove associated volumes"`
}

// SetAutoUpdateInput is the request input for toggling container auto-update.
type SetAutoUpdateInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          struct {
		Enabled bool `json:"enabled" doc:"Whether auto-update is enabled for this container"`
	}
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

type GetContainerEditConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

type EditContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          containertypes.Edit
}

type GenerateComposeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          containertypes.GenerateComposeRequest
}

func RegisterContainers(api huma.API, containerSvc *ContainerService, dockerSvc *docker.DockerClientService, settingsSvc *settings.SettingsService, activitySvc *activity.ActivityService, appCtx handlerutil.ActivityAppContext) {
	h := &ContainerHandler{
		containerService: containerSvc,
		dockerService:    dockerSvc,
		settingsService:  settingsSvc,
		activityService:  activitySvc,
		appCtx:           appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-containers",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers",
		Summary:     "List containers",
		Description: "Paginated list of containers",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersList, h.ListContainers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "container-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/counts",
		Summary:     "Container status counts",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersList, h.GetContainerStatusCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers",
		Summary:     "Create container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersCreate, h.CreateContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-container",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Get container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRead, h.GetContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "start-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/start",
		Summary:     "Start container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersStart, h.StartContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "stop-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/stop",
		Summary:     "Stop container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersStop, h.StopContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "restart-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/restart",
		Summary:     "Restart container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRestart, h.RestartContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "kill-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/kill",
		Summary:     "Kill container",
		Description: "Send a signal to the container's main process (default SIGKILL)",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersKill, h.KillContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pause-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/pause",
		Summary:     "Pause container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersPause, h.PauseContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "unpause-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/unpause",
		Summary:     "Unpause container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersPause, h.UnpauseContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "commit-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/commit",
		Summary:     "Commit container",
		Description: "Create an image from a container",
		Tags:        []string{"Containers", "Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesCommit, h.CommitContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "redeploy-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/redeploy",
		Summary:     "Redeploy container",
		Description: "Pull latest image and recreate container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRedeploy, h.RedeployContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "generate-compose",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/generate-compose",
		Summary:     "Generate compose file",
		Description: "Generate a compose file from existing containers",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRead, h.GenerateCompose)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-container-edit-config",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}/edit-config",
		Summary:     "Get container edit config",
		Description: "Editable configuration snapshot backing the container edit form",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRead, h.GetContainerEditConfig)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "edit-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/edit",
		Summary:     "Edit container",
		Description: "Apply configuration changes and recreate the container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersEdit, h.EditContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-container",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Delete container",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersDelete, h.DeleteContainer)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "set-container-auto-update",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/containers/{containerId}/auto-update",
		Summary:     "Set container auto-update",
		Description: "Enable or disable auto-update for a specific container",
		Tags:        []string{"Containers", "Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersAutoUpdate, h.SetAutoUpdate)
}

func (h *ContainerHandler) ListContainers(ctx context.Context, input *ListContainersInput) (*ListContainersOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}
	if input.Standalone != "" {
		params.Filters["standalone"] = input.Standalone
	}

	result, err := h.containerService.ListContainersPaginated(ctx, params, true, input.IncludeInternal, input.GroupBy)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list containers").Error())
	}

	return &ListContainersOutput{
		Body: ContainerPaginatedResponse{
			Success:    true,
			Data:       result.Items,
			Groups:     result.Groups,
			Counts:     result.Counts,
			Pagination: handlerutil.PaginationResponse(result.Pagination),
		},
	}, nil
}

func (h *ContainerHandler) GetContainerStatusCounts(ctx context.Context, input *GetContainerStatusCountsInput) (*handlerutil.Out[containertypes.StatusCounts], error) {
	containers, _, _, _, err := h.dockerService.GetAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get container counts").Error())
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

	return &handlerutil.Out[containertypes.StatusCounts]{
		Body: base.ApiResponse[containertypes.StatusCounts]{
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
		Healthcheck:     healthConfigFromCreateInternal(body.Healthcheck),
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
	if len(input.CapAdd) > 0 {
		hostConfig.CapAdd = input.CapAdd
	}
	if len(input.CapDrop) > 0 {
		hostConfig.CapDrop = input.CapDrop
	}
	if len(input.Mounts) > 0 {
		hostConfig.Mounts = mountsFromCreateInternal(input.Mounts)
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

func buildNetworkingConfig(body containertypes.Create) (*network.NetworkingConfig, error) {
	if body.NetworkingConfig != nil && len(body.NetworkingConfig.EndpointsConfig) > 0 {
		networkingConfig := &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
		for name, endpoint := range body.NetworkingConfig.EndpointsConfig {
			ipam, err := endpointIPAMFromCreateInternal(endpoint)
			if err != nil {
				return nil, err
			}
			networkingConfig.EndpointsConfig[name] = &network.EndpointSettings{Aliases: endpoint.Aliases, IPAMConfig: ipam}
		}
		return networkingConfig, nil
	}

	if len(body.Networks) > 0 {
		networkingConfig := &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
		for _, net := range body.Networks {
			networkingConfig.EndpointsConfig[net] = &network.EndpointSettings{}
		}
		return networkingConfig, nil
	}

	return nil, nil
}

func (h *ContainerHandler) CreateContainer(ctx context.Context, input *CreateContainerInput) (*handlerutil.Out[containertypes.Created], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	config := buildContainerConfig(input.Body)
	portBindings := network.PortMap{}
	if err := applyLegacyPortBindings(input.Body, config, portBindings); err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Invalid port format").Error())
	}
	if err := applyExposedPorts(input.Body.ExposedPorts, config); err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Invalid port format").Error())
	}

	hostConfig := buildHostConfigBase(input.Body, portBindings)
	if err := applyHostConfigOverrides(input.Body, config, hostConfig, portBindings); err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Invalid port format").Error())
	}
	applyLegacyResourceLimits(input.Body, hostConfig)

	networkingConfig, err := buildNetworkingConfig(input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Invalid network configuration").Error())
	}

	containerJSON, err := h.containerService.CreateContainer(ctx, config, hostConfig, networkingConfig, input.Body.Name, *user, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to create container").Error())
	}

	out := containertypes.Created{
		ID:      containerJSON.ID,
		Name:    containerJSON.Name,
		Image:   containerJSON.Config.Image,
		Status:  string(containerJSON.State.Status),
		Created: containerJSON.Created,
	}

	return &handlerutil.Out[containertypes.Created]{
		Body: base.ApiResponse[containertypes.Created]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *ContainerHandler) GetContainer(ctx context.Context, input *GetContainerInput) (*handlerutil.Out[containertypes.Details], error) {
	details, err := h.containerService.GetContainerDetails(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound(errors.WithMessage(err, "Failed to retrieve container").Error())
	}

	return &handlerutil.Out[containertypes.Details]{
		Body: base.ApiResponse[containertypes.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ContainerHandler) GenerateCompose(ctx context.Context, input *GenerateComposeInput) (*handlerutil.Out[containertypes.GenerateComposeResponse], error) {
	composeContent, err := h.containerService.GenerateCompose(ctx, input.Body.ContainerIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to generate compose file").Error())
	}

	return &handlerutil.Out[containertypes.GenerateComposeResponse]{
		Body: base.ApiResponse[containertypes.GenerateComposeResponse]{
			Success: true,
			Data:    containertypes.GenerateComposeResponse{ComposeContent: composeContent},
		},
	}, nil
}

func (h *ContainerHandler) StartContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    activitytypes.TypeContainerStart,
		Step:            "Starting container",
		StartMessage:    "Container start requested",
		CompleteMessage: "Container started",
		SuccessMessage:  "Container started successfully",
		Action: func(runtimeCtx context.Context, containerID string, user common.User) error {
			return h.containerService.StartContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to start container").Error())
		},
	})
}

func (h *ContainerHandler) StopContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    activitytypes.TypeContainerStop,
		Step:            "Stopping container",
		StartMessage:    "Container stop requested",
		CompleteMessage: "Container stopped",
		SuccessMessage:  "Container stopped successfully",
		Action: func(runtimeCtx context.Context, containerID string, user common.User) error {
			return h.containerService.StopContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to stop container").Error())
		},
	})
}

func (h *ContainerHandler) RestartContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    activitytypes.TypeContainerRestart,
		Step:            "Restarting container",
		StartMessage:    "Container restart requested",
		CompleteMessage: "Container restarted",
		SuccessMessage:  "Container restarted successfully",
		Action: func(runtimeCtx context.Context, containerID string, user common.User) error {
			return h.containerService.RestartContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to restart container").Error())
		},
	})
}

func (h *ContainerHandler) KillContainer(ctx context.Context, input *KillContainerInput) (*handlerutil.Out[base.MessageResponse], error) {
	signal := strings.TrimSpace(input.Signal)
	return h.runContainerActionInternal(ctx, &ContainerActionInput{EnvironmentID: input.EnvironmentID, ContainerID: input.ContainerID}, containerActionConfigInternal{
		ActivityType:    activitytypes.TypeContainerKill,
		Step:            "Killing container",
		StartMessage:    "Container kill requested",
		CompleteMessage: "Container killed",
		SuccessMessage:  "Container killed successfully",
		Action: func(runtimeCtx context.Context, containerID string, user common.User) error {
			return h.containerService.KillContainer(runtimeCtx, containerID, signal, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to kill container").Error())
		},
	})
}

func (h *ContainerHandler) PauseContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    activitytypes.TypeContainerPause,
		Step:            "Pausing container",
		StartMessage:    "Container pause requested",
		CompleteMessage: "Container paused",
		SuccessMessage:  "Container paused successfully",
		Action: func(runtimeCtx context.Context, containerID string, user common.User) error {
			return h.containerService.PauseContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to pause container").Error())
		},
	})
}

func (h *ContainerHandler) UnpauseContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runContainerActionInternal(ctx, input, containerActionConfigInternal{
		ActivityType:    activitytypes.TypeContainerUnpause,
		Step:            "Unpausing container",
		StartMessage:    "Container unpause requested",
		CompleteMessage: "Container unpaused",
		SuccessMessage:  "Container unpaused successfully",
		Action: func(runtimeCtx context.Context, containerID string, user common.User) error {
			return h.containerService.UnpauseContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to unpause container").Error())
		},
	})
}

type containerActionConfigInternal struct {
	ActivityType    activitytypes.Type
	Step            string
	StartMessage    string
	CompleteMessage string
	SuccessMessage  string
	Action          func(context.Context, string, common.User) error
	Error           func(error) error
}

func (h *ContainerHandler) runContainerActionInternal(ctx context.Context, input *ContainerActionInput, cfg containerActionConfigInternal) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(runtimeCtx, h.activityService, input.EnvironmentID, cfg.ActivityType, "container", input.ContainerID, input.ContainerID, user, cfg.Step, cfg.StartMessage, database.JSON{"containerID": input.ContainerID}, false)
	if err := cfg.Action(runtimeCtx, input.ContainerID, *user); err != nil {
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, err)
		return nil, cfg.Error(err)
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, nil)

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: cfg.SuccessMessage, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}

func (h *ContainerHandler) CommitContainer(ctx context.Context, input *CommitContainerInput) (*handlerutil.Out[containertypes.CommitResult], error) {
	if strings.TrimSpace(input.ContainerID) == "" {
		return nil, huma.Error400BadRequest("container ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	out, err := h.containerService.CommitContainer(ctx, input.ContainerID, input.Body, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to commit container: %v", err))
	}

	return &handlerutil.Out[containertypes.CommitResult]{
		Body: base.ApiResponse[containertypes.CommitResult]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

func (h *ContainerHandler) RedeployContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[containertypes.Details], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(runtimeCtx, h.activityService, input.EnvironmentID, activitytypes.TypeContainerRedeploy, "container", input.ContainerID, input.ContainerID, user, "Starting redeploy", "Container redeploy requested", database.JSON{"containerID": input.ContainerID}, true)
	activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, input.EnvironmentID)
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Redeploying container")
	redeployCtx := context.WithValue(runtimeCtx, dockerutils.ProgressWriterKey{}, activityWriter)
	newContainerID, err := h.containerService.RedeployContainer(redeployCtx, input.ContainerID, *user)
	if err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeploy failed", err)
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to redeploy container").Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeployed", nil)

	// Fetch full container details to return (consistent with other endpoints)
	details, inspectErr := h.containerService.GetContainerDetails(runtimeCtx, newContainerID)
	if inspectErr == nil {
		details.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

		return &handlerutil.Out[containertypes.Details]{
			Body: base.ApiResponse[containertypes.Details]{
				Success: true,
				Data:    details,
			},
		}, nil
	}

	// Container was redeployed successfully, but we couldn't fetch full details.
	// Return minimal response with just the ID so frontend can still navigate.
	return &handlerutil.Out[containertypes.Details]{
		Body: base.ApiResponse[containertypes.Details]{
			Success: true,
			Data: containertypes.Details{
				ID:         newContainerID,
				ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

func (h *ContainerHandler) GetContainerEditConfig(ctx context.Context, input *GetContainerEditConfigInput) (*handlerutil.Out[containertypes.EditConfig], error) {
	editConfig, err := h.containerService.GetContainerEditConfig(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound(errors.WithMessage(err, "Failed to retrieve container").Error())
	}

	return &handlerutil.Out[containertypes.EditConfig]{
		Body: base.ApiResponse[containertypes.EditConfig]{
			Success: true,
			Data:    editConfig,
		},
	}, nil
}

func editContainerHTTPErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrValidation):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError(errors.WithMessage(err, "Failed to edit container").Error())
	}
}

func (h *ContainerHandler) EditContainer(ctx context.Context, input *EditContainerInput) (*handlerutil.Out[containertypes.Details], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(runtimeCtx, h.activityService, input.EnvironmentID, activitytypes.TypeContainerEdit, "container", input.ContainerID, input.ContainerID, user, "Starting edit", "Container edit requested", database.JSON{"containerID": input.ContainerID}, true)
	activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, input.EnvironmentID)
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Editing container")
	editCtx := context.WithValue(runtimeCtx, dockerutils.ProgressWriterKey{}, activityWriter)
	newContainerID, err := h.containerService.EditContainer(editCtx, input.ContainerID, input.Body, *user)
	if err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container edit failed", err)
		return nil, editContainerHTTPErrorInternal(err)
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container edited", nil)

	// Fetch full container details to return (consistent with redeploy)
	details, inspectErr := h.containerService.GetContainerDetails(runtimeCtx, newContainerID)
	if inspectErr == nil {
		details.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

		return &handlerutil.Out[containertypes.Details]{
			Body: base.ApiResponse[containertypes.Details]{
				Success: true,
				Data:    details,
			},
		}, nil
	}

	// Container was recreated successfully, but we couldn't fetch full details.
	// Return minimal response with just the ID so frontend can still navigate.
	return &handlerutil.Out[containertypes.Details]{
		Body: base.ApiResponse[containertypes.Details]{
			Success: true,
			Data: containertypes.Details{
				ID:         newContainerID,
				ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

func (h *ContainerHandler) DeleteContainer(ctx context.Context, input *DeleteContainerInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(runtimeCtx, h.activityService, input.EnvironmentID, activitytypes.TypeContainerDelete, "container", input.ContainerID, input.ContainerID, user, "Deleting container", "Container delete requested", database.JSON{"containerID": input.ContainerID, "force": input.Force, "removeVolumes": input.RemoveVolumes}, false)
	if err := h.containerService.DeleteContainer(runtimeCtx, input.ContainerID, input.Force, input.RemoveVolumes, *user); err != nil {
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container deleted", err)
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to delete container").Error())
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container deleted", nil)

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Container deleted successfully", ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}

func (h *ContainerHandler) SetAutoUpdate(ctx context.Context, input *SetAutoUpdateInput) (*handlerutil.Out[base.MessageResponse], error) {
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

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: msg},
		},
	}, nil
}
