package container

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/netip"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/container"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/samber/mo"
	"go.getarcane.app/docker/types"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container/children/stats"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
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
	Success               bool                     `json:"success"`
	Data                  []container.Summary      `json:"data"`
	Groups                []container.SummaryGroup `json:"groups,omitempty"`
	Counts                container.StatusCounts   `json:"counts"`
	Pagination            base.PaginationResponse  `json:"pagination"`
	ResourceSortSupported bool                     `json:"resourceSortSupported"`
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
	IncludeHidden   bool   `query:"includeHidden" default:"false" doc:"Include hidden containers"`
	Updates         string `query:"updates" doc:"Filter by update status (has_update, up_to_date, error, unknown)"`
	Standalone      string `query:"standalone" doc:"Filter standalone containers only (true/false)"`
	Label           string `query:"label" doc:"Filter by label key or key=value"`
}

type ListContainersOutput struct {
	Body ContainerPaginatedResponse
}

type GetContainerStatusCountsInput struct {
	EnvironmentID   string `path:"id" doc:"Environment ID"`
	IncludeInternal bool   `query:"includeInternal" default:"false" doc:"Include internal containers"`
	IncludeHidden   bool   `query:"includeHidden" default:"false" doc:"Include hidden containers"`
}

type CreateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          container.Create
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
	Body          container.CommitRequest
}

type GetContainerEditConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

type EditContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          container.Edit
}

type GenerateComposeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          container.GenerateComposeRequest
}

func (h *ContainerHandler) ListContainers(ctx context.Context, input *ListContainersInput) (*ListContainersOutput, error) {
	if ps, _ := middleware.PermissionsFromContext(ctx); stats.ContainerResourceSortPermissionDenied(ps, input.EnvironmentID, input.Sort) {
		return nil, huma.Error403Forbidden("permission denied: " + authz.PermContainersRead)
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}
	if input.Standalone != "" {
		params.Filters["standalone"] = input.Standalone
	}
	if input.Label != "" {
		params.Filters["label"] = input.Label
	}

	result, err := h.containerService.ListContainersPaginated(ctx, params, true, input.IncludeInternal, input.IncludeHidden, input.GroupBy)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list containers: " + err.Error())
	}

	return &ListContainersOutput{
		Body: ContainerPaginatedResponse{
			Success:               true,
			Data:                  result.Items,
			Groups:                result.Groups,
			Counts:                result.Counts,
			Pagination:            handlerutil.PaginationResponse(result.Pagination),
			ResourceSortSupported: true,
		},
	}, nil
}

func (h *ContainerHandler) GetContainerStatusCounts(ctx context.Context, input *GetContainerStatusCountsInput) (*handlerutil.Out[container.StatusCounts], error) {
	containers, _, _, _, err := h.dockerService.GetAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get container counts: " + err.Error())
	}

	containers = FilterExcludedContainers(containers, input.IncludeInternal, input.IncludeHidden)

	running, stopped := 0, 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}
	total := len(containers)

	return &handlerutil.Out[container.StatusCounts]{
		Body: base.ApiResponse[container.StatusCounts]{
			Success: true,
			Data: container.StatusCounts{
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

func buildCreateLabels(body container.Create) map[string]string {
	labels := map[string]string{
		"com.arcane.created": "true",
	}
	maps.Copy(labels, body.Labels)

	return labels
}

func buildContainerConfig(body container.Create) *dockercontainer.Config {
	return &dockercontainer.Config{
		Image:           body.Image,
		Cmd:             kit.Ternary(len(body.Command) > 0, body.Command, body.Cmd),
		Entrypoint:      body.Entrypoint,
		WorkingDir:      body.WorkingDir,
		User:            body.User,
		Env:             kit.Ternary(len(body.Environment) > 0, body.Environment, body.Env),
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

func applyLegacyPortBindings(body container.Create, config *dockercontainer.Config, portBindings network.PortMap) error {
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

func buildHostConfigBase(body container.Create, portBindings network.PortMap) *dockercontainer.HostConfig {
	return &dockercontainer.HostConfig{
		Binds:         body.Volumes,
		PortBindings:  portBindings,
		Privileged:    body.Privileged,
		AutoRemove:    body.AutoRemove,
		RestartPolicy: dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyMode(body.RestartPolicy)},
	}
}

func applyHostConfigPortBindings(config *dockercontainer.Config, portBindings network.PortMap, bindings map[string][]container.PortBindingCreate) error {
	for portSpec, bindingList := range bindings {
		port, err := parsePortSpec(portSpec)
		if err != nil {
			return err
		}
		config.ExposedPorts[port] = struct{}{}
		for _, binding := range bindingList {
			pb := network.PortBinding{HostPort: binding.HostPort}
			if hostIP := strings.TrimSpace(binding.HostIP); hostIP != "" {
				parsedIP, parseAddrErr := netip.ParseAddr(hostIP)
				if parseAddrErr != nil {
					return parseAddrErr
				}
				pb.HostIP = parsedIP
			}
			portBindings[port] = append(portBindings[port], pb)
		}
	}

	return nil
}

func applyHostConfigSettings(hostConfig *dockercontainer.HostConfig, input *container.HostConfigCreate) {
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

func applyHostConfigOverrides(body container.Create, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, portBindings network.PortMap) error {
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

func applyLegacyResourceLimits(body container.Create, hostConfig *dockercontainer.HostConfig) {
	if body.Memory > 0 {
		hostConfig.Memory = body.Memory
	}
	if body.CPUs > 0 {
		hostConfig.NanoCPUs = int64(body.CPUs * 1e9)
	}
}

func buildNetworkingConfig(body container.Create) (*network.NetworkingConfig, error) {
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

func (h *ContainerHandler) CreateContainer(ctx context.Context, input *CreateContainerInput) (*handlerutil.Out[container.Created], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	config := buildContainerConfig(input.Body)
	portBindings := network.PortMap{}
	if applyLegacyPortBindingsErr := applyLegacyPortBindings(input.Body, config, portBindings); applyLegacyPortBindingsErr != nil {
		return nil, huma.Error400BadRequest("Invalid port format: " + applyLegacyPortBindingsErr.Error())
	}
	if applyExposedPortsErr := applyExposedPorts(input.Body.ExposedPorts, config); applyExposedPortsErr != nil {
		return nil, huma.Error400BadRequest("Invalid port format: " + applyExposedPortsErr.Error())
	}

	hostConfig := buildHostConfigBase(input.Body, portBindings)
	if applyHostConfigOverridesErr := applyHostConfigOverrides(input.Body, config, hostConfig, portBindings); applyHostConfigOverridesErr != nil {
		return nil, huma.Error400BadRequest("Invalid port format: " + applyHostConfigOverridesErr.Error())
	}
	applyLegacyResourceLimits(input.Body, hostConfig)

	networkingConfig, err := buildNetworkingConfig(input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid network configuration: " + err.Error())
	}

	containerJSON, err := h.containerService.CreateContainer(ctx, config, hostConfig, networkingConfig, input.Body.Name, *user, input.Body.Credentials)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create container: " + err.Error())
	}

	out := container.Created{
		ID:      containerJSON.ID,
		Name:    containerJSON.Name,
		Image:   containerJSON.Config.Image,
		Status:  string(containerJSON.State.Status),
		Created: containerJSON.Created,
	}

	return &handlerutil.Out[container.Created]{
		Body: base.ApiResponse[container.Created]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *ContainerHandler) GetContainer(ctx context.Context, input *GetContainerInput) (*handlerutil.Out[container.Details], error) {
	details, err := h.containerService.GetContainerDetails(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound("Failed to retrieve container: " + err.Error())
	}

	return &handlerutil.Out[container.Details]{
		Body: base.ApiResponse[container.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ContainerHandler) GetContainerProcesses(ctx context.Context, input *GetContainerInput) (*handlerutil.Out[container.Processes], error) {
	processes, err := h.containerService.GetContainerProcesses(ctx, input.ContainerID)
	if err != nil {
		message := "Failed to retrieve container processes: " + err.Error()
		switch {
		case errdefs.IsNotFound(err):
			return nil, huma.Error404NotFound(message)
		case errdefs.IsConflict(err):
			return nil, huma.Error409Conflict(message)
		case errors.Is(err, context.DeadlineExceeded):
			return nil, huma.Error504GatewayTimeout(message)
		default:
			return nil, huma.Error500InternalServerError(message)
		}
	}

	return &handlerutil.Out[container.Processes]{
		Body: base.ApiResponse[container.Processes]{
			Success: true,
			Data:    processes,
		},
	}, nil
}

func (h *ContainerHandler) DownloadContainerLogs(ctx context.Context, input *GetContainerInput) (*huma.StreamResponse, error) {
	reader, filename, err := h.containerService.DownloadLogs(ctx, input.ContainerID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Failed to retrieve container: " + err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to download container logs: " + err.Error())
	}
	return handlerutil.DownloadResponse(reader, -1, filename), nil
}

func (h *ContainerHandler) GenerateCompose(ctx context.Context, input *GenerateComposeInput) (*handlerutil.Out[container.GenerateComposeResponse], error) {
	composeContent, err := projects.ComposeGenerate(ctx, h.dockerService.DockerHost(), "", input.Body.ContainerIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to generate compose file: " + err.Error())
	}

	return &handlerutil.Out[container.GenerateComposeResponse]{
		Body: base.ApiResponse[container.GenerateComposeResponse]{
			Success: true,
			Data:    container.GenerateComposeResponse{ComposeContent: composeContent},
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
		Action: func(runtimeCtx context.Context, containerID string, user usertypes.Actor) error {
			return h.containerService.StartContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to start container: " + err.Error())
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
		Action: func(runtimeCtx context.Context, containerID string, user usertypes.Actor) error {
			return h.containerService.StopContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to stop container: " + err.Error())
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
		Action: func(runtimeCtx context.Context, containerID string, user usertypes.Actor) error {
			return h.containerService.RestartContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to restart container: " + err.Error())
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
		Action: func(runtimeCtx context.Context, containerID string, user usertypes.Actor) error {
			return h.containerService.KillContainer(runtimeCtx, containerID, signal, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to kill container: " + err.Error())
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
		Action: func(runtimeCtx context.Context, containerID string, user usertypes.Actor) error {
			return h.containerService.PauseContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to pause container: " + err.Error())
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
		Action: func(runtimeCtx context.Context, containerID string, user usertypes.Actor) error {
			return h.containerService.UnpauseContainer(runtimeCtx, containerID, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to unpause container: " + err.Error())
		},
	})
}

type containerActionConfigInternal struct {
	ActivityType    activitytypes.Type
	Step            string
	StartMessage    string
	CompleteMessage string
	SuccessMessage  string
	Action          func(context.Context, string, usertypes.Actor) error
	Error           func(error) error
}

// containerActivityNameInternal returns the container's name for activity records, falling back to its ID.
func (h *ContainerHandler) containerActivityNameInternal(ctx context.Context, containerID string) string {
	name, err := h.containerService.GetContainerNameByID(ctx, containerID)
	if err != nil || name == "" {
		return containerID
	}
	return name
}

func (h *ContainerHandler) runContainerActionInternal(ctx context.Context, input *ContainerActionInput, cfg containerActionConfigInternal) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		cfg.ActivityType,
		"container",
		input.ContainerID,
		h.containerActivityNameInternal(
			runtimeCtx,
			input.ContainerID,
		),
		user,
		cfg.Step,
		cfg.StartMessage,
		database.JSON{
			"containerID": input.ContainerID,
		},
		false,
	)
	if actionErr := cfg.Action(runtimeCtx, input.ContainerID, *user); actionErr != nil {
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, actionErr)
		return nil, cfg.Error(actionErr)
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, nil)

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: cfg.SuccessMessage, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()},
		},
	}, nil
}

func (h *ContainerHandler) CommitContainer(ctx context.Context, input *CommitContainerInput) (*handlerutil.Out[container.CommitResult], error) {
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

	return &handlerutil.Out[container.CommitResult]{
		Body: base.ApiResponse[container.CommitResult]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

func (h *ContainerHandler) RedeployContainer(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[container.Details], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		activitytypes.TypeContainerRedeploy,
		"container",
		input.ContainerID,
		h.containerActivityNameInternal(
			runtimeCtx,
			input.ContainerID,
		),
		user,
		"Starting redeploy",
		"Container redeploy requested",
		database.JSON{
			"containerID": input.ContainerID,
		},
		true,
	)
	activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, input.EnvironmentID)
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Redeploying container")
	redeployCtx := context.WithValue(runtimeCtx, types.ProgressWriterKey{}, activityWriter)
	newContainerID, err := h.containerService.RedeployContainer(redeployCtx, input.ContainerID, *user)
	if err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeploy failed", err)
		return nil, huma.Error500InternalServerError("Failed to redeploy container: " + err.Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeployed", nil)

	// Fetch full container details to return (consistent with other endpoints)
	details, inspectErr := h.containerService.GetContainerDetails(runtimeCtx, newContainerID)
	if inspectErr == nil {
		details.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

		return &handlerutil.Out[container.Details]{
			Body: base.ApiResponse[container.Details]{
				Success: true,
				Data:    details,
			},
		}, nil
	}

	// Container was redeployed successfully, but we couldn't fetch full details.
	// Return minimal response with just the ID so frontend can still navigate.
	return &handlerutil.Out[container.Details]{
		Body: base.ApiResponse[container.Details]{
			Success: true,
			Data: container.Details{
				ID:         newContainerID,
				ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

func (h *ContainerHandler) GetContainerEditConfig(ctx context.Context, input *GetContainerEditConfigInput) (*handlerutil.Out[container.EditConfig], error) {
	editConfig, err := h.containerService.GetContainerEditConfig(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound("Failed to retrieve container: " + err.Error())
	}

	return &handlerutil.Out[container.EditConfig]{
		Body: base.ApiResponse[container.EditConfig]{
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
		return huma.Error500InternalServerError("Failed to edit container: " + err.Error())
	}
}

func (h *ContainerHandler) EditContainer(ctx context.Context, input *EditContainerInput) (*handlerutil.Out[container.Details], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		activitytypes.TypeContainerEdit,
		"container",
		input.ContainerID,
		h.containerActivityNameInternal(
			runtimeCtx,
			input.ContainerID,
		),
		user,
		"Starting edit",
		"Container edit requested",
		database.JSON{
			"containerID": input.ContainerID,
		},
		true,
	)
	activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, input.EnvironmentID)
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Editing container")
	editCtx := context.WithValue(runtimeCtx, types.ProgressWriterKey{}, activityWriter)
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

		return &handlerutil.Out[container.Details]{
			Body: base.ApiResponse[container.Details]{
				Success: true,
				Data:    details,
			},
		}, nil
	}

	// Container was recreated successfully, but we couldn't fetch full details.
	// Return minimal response with just the ID so frontend can still navigate.
	return &handlerutil.Out[container.Details]{
		Body: base.ApiResponse[container.Details]{
			Success: true,
			Data: container.Details{
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
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		activitytypes.TypeContainerDelete,
		"container",
		input.ContainerID,
		h.containerActivityNameInternal(
			runtimeCtx,
			input.ContainerID,
		),
		user,
		"Deleting container",
		"Container delete requested",
		database.JSON{
			"containerID":   input.ContainerID,
			"force":         input.Force,
			"removeVolumes": input.RemoveVolumes,
		},
		false,
	)
	if deleteContainerErr := h.containerService.DeleteContainer(runtimeCtx, input.ContainerID, input.Force, input.RemoveVolumes, *user); deleteContainerErr != nil {
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container deleted", deleteContainerErr)
		return nil, huma.Error500InternalServerError("Failed to delete container: " + deleteContainerErr.Error())
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
	if setContainerAutoUpdateExclusionErr := h.settingsService.SetContainerAutoUpdateExclusionInternal(ctx, containerName, excluded); setContainerAutoUpdateExclusionErr != nil {
		return nil, huma.Error500InternalServerError("failed to update auto-update setting")
	}

	msg := kit.Ternary(excluded, "Auto-update disabled", "Auto-update enabled")

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: msg},
		},
	}, nil
}
