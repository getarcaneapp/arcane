package container

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/samber/mo"
	dockertypes "go.getarcane.app/docker/types"
	kit "go.getarcane.app/kit/pkg"

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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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
	Success               bool                          `json:"success"`
	Data                  []types.ContainerSummary      `json:"data"`
	Groups                []types.ContainerSummaryGroup `json:"groups,omitempty"`
	Counts                types.ContainerStatusCounts   `json:"counts"`
	Pagination            types.PaginationResponse      `json:"pagination"`
	ResourceSortSupported bool                          `json:"resourceSortSupported"`
	FailedEnvironments    []types.EnvironmentFailure    `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
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
	types.Create[types.Container]
}

type GetContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

type ContainerActionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Action        string `query:"action" required:"true" enum:"start,stop,restart,kill,pause,unpause" doc:"Lifecycle action"`
	Signal        string `query:"signal" doc:"Signal to send for kill (for example SIGTERM, SIGKILL). Defaults to SIGKILL."`
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
	Body          types.ContainerSetAutoUpdateRequest
}

type CommitContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          types.ContainerCommitRequest
}

type GetContainerEditConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
}

type EditContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID"`
	Body          types.ContainerEdit
}

type GenerateComposeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.ContainerGenerateComposeRequest
}

type containerActionConfig struct {
	ActivityType    types.ActivityType
	Verb            string
	Step            string
	StartMessage    string
	CompleteMessage string
	SuccessMessage  string
	Action          func(context.Context, string, types.Actor) error
}

func (h *ContainerHandler) ListContainers(ctx context.Context, input *ListContainersInput) (*ListContainersOutput, error) {
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

	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.containerService.AggregateContainers(ctx, params, input.GroupBy)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list containers: " + err.Error())
		}
		return &ListContainersOutput{Body: body}, nil
	}

	if ps, _ := middleware.PermissionsFromContext(ctx); stats.ContainerResourceSortPermissionDenied(ps, input.EnvironmentID, input.Sort) {
		return nil, huma.Error403Forbidden("permission denied: " + authz.PermContainersRead)
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

func (h *ContainerHandler) GetContainerStatusCounts(ctx context.Context, input *GetContainerStatusCountsInput) (*handlerutil.Out[types.ContainerStatusCounts], error) {
	containers, _, _, _, err := h.dockerService.GetAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get container counts: " + err.Error())
	}

	containers = FilterExcludedContainers(containers, input.IncludeInternal, input.IncludeHidden)

	running := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		}
	}

	return &handlerutil.Out[types.ContainerStatusCounts]{
		Body: types.ApiResponse[types.ContainerStatusCounts]{
			Success: true,
			Data: types.ContainerStatusCounts{
				RunningContainers: running,
				StoppedContainers: len(containers) - running,
				TotalContainers:   len(containers),
			},
		},
	}, nil
}

func (h *ContainerHandler) CreateContainer(ctx context.Context, input *CreateContainerInput) (*handlerutil.Out[types.ContainerCreated], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	exposedPorts := network.PortSet{}
	portBindings := network.PortMap{}
	for containerPort, hostPort := range input.Body.Ports {
		port, parsePortErr := network.ParsePort(containerPort + "/tcp")
		if parsePortErr != nil {
			return nil, huma.Error400BadRequest("Invalid port format: " + parsePortErr.Error())
		}
		exposedPorts[port] = struct{}{}
		portBindings[port] = []network.PortBinding{{HostPort: hostPort}}
	}
	for portSpec := range input.Body.ExposedPorts {
		port, parsePortErr := network.ParsePort(kit.Ternary(strings.Contains(portSpec, "/"), portSpec, portSpec+"/tcp"))
		if parsePortErr != nil {
			return nil, huma.Error400BadRequest("Invalid port format: " + parsePortErr.Error())
		}
		exposedPorts[port] = struct{}{}
	}

	hostConfig := kit.FromPtr(input.Body.HostConfig)
	hostBindings, err := portMapFromCreate(hostConfig.PortBindings)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid port format: " + err.Error())
	}
	for port, bindings := range hostBindings {
		exposedPorts[port] = struct{}{}
		if len(bindings) > 0 {
			portBindings[port] = append(portBindings[port], bindings...)
		}
	}

	endpoints := map[string]*network.EndpointSettings{}
	for name, endpoint := range kit.FromPtr(input.Body.NetworkingConfig).EndpointsConfig {
		ipam, ipamErr := endpointIPAMFromCreate(endpoint)
		if ipamErr != nil {
			return nil, huma.Error400BadRequest("Invalid network configuration: " + ipamErr.Error())
		}
		endpoints[name] = &network.EndpointSettings{Aliases: endpoint.Aliases, IPAMConfig: ipam}
	}
	if len(endpoints) == 0 {
		for _, networkName := range input.Body.Networks {
			endpoints[networkName] = &network.EndpointSettings{}
		}
	}

	createLabels := map[string]string{"com.arcane.created": "true"}
	maps.Copy(createLabels, input.Body.Labels)

	containerJSON, err := h.containerService.CreateContainer(
		ctx,
		&dockercontainer.Config{
			Image:           input.Body.Image,
			Cmd:             kit.Ternary(len(input.Body.Command) > 0, input.Body.Command, input.Body.Cmd),
			Entrypoint:      input.Body.Entrypoint,
			WorkingDir:      input.Body.WorkingDir,
			User:            input.Body.User,
			Env:             kit.Ternary(len(input.Body.Environment) > 0, input.Body.Environment, input.Body.Env),
			ExposedPorts:    exposedPorts,
			Labels:          createLabels,
			Healthcheck:     healthConfigFromCreate(input.Body.Healthcheck),
			Hostname:        input.Body.Hostname,
			Domainname:      input.Body.Domainname,
			AttachStdout:    input.Body.AttachStdout,
			AttachStderr:    input.Body.AttachStderr,
			AttachStdin:     input.Body.AttachStdin,
			Tty:             input.Body.Tty,
			OpenStdin:       input.Body.OpenStdin,
			StdinOnce:       input.Body.StdinOnce,
			NetworkDisabled: input.Body.NetworkDisabled,
		},
		&dockercontainer.HostConfig{
			Binds:        kit.Ternary(len(hostConfig.Binds) > 0, hostConfig.Binds, input.Body.Volumes),
			NetworkMode:  dockercontainer.NetworkMode(hostConfig.NetworkMode),
			PortBindings: portBindings,
			RestartPolicy: dockercontainer.RestartPolicy{
				Name:              dockercontainer.RestartPolicyMode(kit.Ternary(hostConfig.RestartPolicy != nil, kit.FromPtr(hostConfig.RestartPolicy).Name, input.Body.RestartPolicy)),
				MaximumRetryCount: kit.FromPtr(hostConfig.RestartPolicy).MaximumRetryCount,
			},
			AutoRemove:      mo.PointerToOption(hostConfig.AutoRemove).OrElse(input.Body.AutoRemove),
			CapAdd:          kit.Ternary(len(hostConfig.CapAdd) > 0, hostConfig.CapAdd, nil),
			CapDrop:         kit.Ternary(len(hostConfig.CapDrop) > 0, hostConfig.CapDrop, nil),
			Privileged:      mo.PointerToOption(hostConfig.Privileged).OrElse(input.Body.Privileged),
			PublishAllPorts: kit.FromPtr(hostConfig.PublishAllPorts),
			ReadonlyRootfs:  kit.FromPtr(hostConfig.ReadonlyRootfs),
			Memory:          kit.Ternary(input.Body.Memory > 0, input.Body.Memory, max(hostConfig.Memory, 0)),
			MemorySwap:      max(hostConfig.MemorySwap, 0),
			NanoCPUs:        kit.Ternary(input.Body.CPUs > 0, int64(input.Body.CPUs*1e9), max(hostConfig.NanoCPUs, 0)),
			CPUShares:       max(hostConfig.CPUShares, 0),
			Mounts:          kit.Ternary(len(hostConfig.Mounts) > 0, mergeEditMounts(nil, hostConfig.Mounts), nil),
		},
		func() *network.NetworkingConfig {
			if len(endpoints) == 0 {
				return nil
			}
			return &network.NetworkingConfig{EndpointsConfig: endpoints}
		}(),
		input.Body.Name,
		*user,
		input.Body.Credentials,
	)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[types.ContainerCreated]{
		Body: types.ApiResponse[types.ContainerCreated]{
			Success: true,
			Data: types.ContainerCreated{
				ID:      containerJSON.ID,
				Name:    containerJSON.Name,
				Image:   containerJSON.Config.Image,
				Status:  string(containerJSON.State.Status),
				Created: containerJSON.Created,
			},
		},
	}, nil
}

func (h *ContainerHandler) GetContainer(ctx context.Context, input *GetContainerInput) (*handlerutil.Out[types.ContainerDetails], error) {
	details, err := h.containerService.GetContainerDetails(ctx, input.ContainerID)
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve container: "+apiErr.Message)
	}

	return &handlerutil.Out[types.ContainerDetails]{
		Body: types.ApiResponse[types.ContainerDetails]{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ContainerHandler) GetContainerProcesses(ctx context.Context, input *GetContainerInput) (*handlerutil.Out[types.ContainerProcesses], error) {
	processes, err := h.containerService.GetContainerProcesses(ctx, input.ContainerID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve container processes: "+apiErr.Message)
	}

	return &handlerutil.Out[types.ContainerProcesses]{
		Body: types.ApiResponse[types.ContainerProcesses]{
			Success: true,
			Data:    processes,
		},
	}, nil
}

func (h *ContainerHandler) DownloadContainerLogs(ctx context.Context, input *GetContainerInput) (*huma.StreamResponse, error) {
	reader, filename, err := h.containerService.DownloadLogs(ctx, input.ContainerID)
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to download container logs: "+apiErr.Message)
	}
	return handlerutil.DownloadResponse(reader, -1, filename), nil
}

func (h *ContainerHandler) GenerateCompose(ctx context.Context, input *GenerateComposeInput) (*handlerutil.Out[types.ContainerGenerateComposeResponse], error) {
	composeContent, err := projects.ComposeGenerate(ctx, h.dockerService.DockerHost(), "", input.Body.ContainerIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to generate compose file: " + err.Error())
	}

	return &handlerutil.Out[types.ContainerGenerateComposeResponse]{
		Body: types.ApiResponse[types.ContainerGenerateComposeResponse]{
			Success: true,
			Data:    types.ContainerGenerateComposeResponse{ComposeContent: composeContent},
		},
	}, nil
}

func (h *ContainerHandler) RunContainerAction(ctx context.Context, input *ContainerActionInput) (*handlerutil.Out[types.MessageResponse], error) {
	cfg := map[string]containerActionConfig{
		"start": {
			ActivityType:    types.ActivityTypeContainerStart,
			Verb:            "start",
			Step:            "Starting container",
			StartMessage:    "Container start requested",
			CompleteMessage: "Container started",
			SuccessMessage:  "Container started successfully",
			Action:          h.containerService.StartContainer,
		},
		"stop": {
			ActivityType:    types.ActivityTypeContainerStop,
			Verb:            "stop",
			Step:            "Stopping container",
			StartMessage:    "Container stop requested",
			CompleteMessage: "Container stopped",
			SuccessMessage:  "Container stopped successfully",
			Action:          h.containerService.StopContainer,
		},
		"restart": {
			ActivityType:    types.ActivityTypeContainerRestart,
			Verb:            "restart",
			Step:            "Restarting container",
			StartMessage:    "Container restart requested",
			CompleteMessage: "Container restarted",
			SuccessMessage:  "Container restarted successfully",
			Action:          h.containerService.RestartContainer,
		},
		"kill": {
			ActivityType:    types.ActivityTypeContainerKill,
			Verb:            "kill",
			Step:            "Killing container",
			StartMessage:    "Container kill requested",
			CompleteMessage: "Container killed",
			SuccessMessage:  "Container killed successfully",
			Action: func(runtimeCtx context.Context, containerID string, user types.Actor) error {
				return h.containerService.KillContainer(runtimeCtx, containerID, strings.TrimSpace(input.Signal), user)
			},
		},
		"pause": {
			ActivityType:    types.ActivityTypeContainerPause,
			Verb:            "pause",
			Step:            "Pausing container",
			StartMessage:    "Container pause requested",
			CompleteMessage: "Container paused",
			SuccessMessage:  "Container paused successfully",
			Action:          h.containerService.PauseContainer,
		},
		"unpause": {
			ActivityType:    types.ActivityTypeContainerUnpause,
			Verb:            "unpause",
			Step:            "Unpausing container",
			StartMessage:    "Container unpause requested",
			CompleteMessage: "Container unpaused",
			SuccessMessage:  "Container unpaused successfully",
			Action:          h.containerService.UnpauseContainer,
		},
	}[input.Action]
	return h.runContainerAction(ctx, input, cfg)
}

// containerActivityName returns the container's name for activity records, falling back to its ID.
func (h *ContainerHandler) containerActivityName(ctx context.Context, containerID string) string {
	name, err := h.containerService.GetContainerNameByID(ctx, containerID)
	if err != nil || name == "" {
		return containerID
	}
	return name
}

func (h *ContainerHandler) runContainerAction(ctx context.Context, input *ContainerActionInput, cfg containerActionConfig) (*handlerutil.Out[types.MessageResponse], error) {
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
		h.containerActivityName(
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
		apiErr := common.ToAPIError(common.ClassifyDockerError(actionErr))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to "+cfg.Verb+" container: "+apiErr.Message)
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.CompleteMessage, nil)

	return handlerutil.MessageOutput(cfg.SuccessMessage, activityID), nil
}

func (h *ContainerHandler) CommitContainer(ctx context.Context, input *CommitContainerInput) (*handlerutil.Out[types.ContainerCommitResult], error) {
	if strings.TrimSpace(input.ContainerID) == "" {
		return nil, huma.Error400BadRequest("container ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	out, err := h.containerService.CommitContainer(ctx, input.ContainerID, input.Body, *user)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(common.ClassifyDockerError(err)).HTTPStatus(), fmt.Sprintf("failed to commit container: %v", err))
	}

	return &handlerutil.Out[types.ContainerCommitResult]{
		Body: types.ApiResponse[types.ContainerCommitResult]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

func (h *ContainerHandler) RedeployContainer(ctx context.Context, input *GetContainerInput) (*handlerutil.Out[types.ContainerDetails], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		types.ActivityTypeContainerRedeploy,
		"container",
		input.ContainerID,
		h.containerActivityName(
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
	redeployCtx := context.WithValue(runtimeCtx, dockertypes.ProgressWriterKey{}, activityWriter)
	newContainerID, err := h.containerService.RedeployContainer(redeployCtx, input.ContainerID, *user)
	if err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeploy failed", err)
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to redeploy container: "+apiErr.Message)
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container redeployed", nil)

	// Fetch full container details to return (consistent with other endpoints)
	details, inspectErr := h.containerService.GetContainerDetails(runtimeCtx, newContainerID)
	if inspectErr != nil {
		// Container was redeployed successfully, but we couldn't fetch full details.
		// Return minimal response with just the ID so frontend can still navigate.
		return &handlerutil.Out[types.ContainerDetails]{
			Body: types.ApiResponse[types.ContainerDetails]{
				Success: true,
				Data: types.ContainerDetails{
					ID:         newContainerID,
					ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
				},
			},
		}, nil
	}

	return &handlerutil.Out[types.ContainerDetails]{
		Body: types.ApiResponse[types.ContainerDetails]{
			Success: true,
			Data: types.ContainerDetails{ //exhaustruct:enforce
				ID:                details.ID,
				Name:              details.Name,
				Image:             details.Image,
				ImageID:           details.ImageID,
				Created:           details.Created,
				State:             details.State,
				Config:            details.Config,
				HostConfig:        details.HostConfig,
				NetworkSettings:   details.NetworkSettings,
				Ports:             details.Ports,
				Mounts:            details.Mounts,
				Labels:            details.Labels,
				ComposeInfo:       details.ComposeInfo,
				IconLightURL:      details.IconLightURL,
				IconDarkURL:       details.IconDarkURL,
				RedeployDisabled:  details.RedeployDisabled,
				AutoUpdateEnabled: details.AutoUpdateEnabled,
				UpdateInfo:        details.UpdateInfo,
				ActivityID:        mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

func (h *ContainerHandler) GetContainerEditConfig(ctx context.Context, input *GetContainerEditConfigInput) (*handlerutil.Out[types.ContainerEditConfig], error) {
	editConfig, err := h.containerService.GetContainerEditConfig(ctx, input.ContainerID)
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve container: "+apiErr.Message)
	}

	return &handlerutil.Out[types.ContainerEditConfig]{
		Body: types.ApiResponse[types.ContainerEditConfig]{
			Success: true,
			Data:    editConfig,
		},
	}, nil
}

func (h *ContainerHandler) EditContainer(ctx context.Context, input *EditContainerInput) (*handlerutil.Out[types.ContainerDetails], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		types.ActivityTypeContainerEdit,
		"container",
		input.ContainerID,
		h.containerActivityName(
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
	editCtx := context.WithValue(runtimeCtx, dockertypes.ProgressWriterKey{}, activityWriter)
	newContainerID, err := h.containerService.EditContainer(editCtx, input.ContainerID, input.Body, *user)
	if err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container edit failed", err)
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container edited", nil)

	// Fetch full container details to return (consistent with redeploy)
	details, inspectErr := h.containerService.GetContainerDetails(runtimeCtx, newContainerID)
	if inspectErr != nil {
		// Container was recreated successfully, but we couldn't fetch full details.
		// Return minimal response with just the ID so frontend can still navigate.
		return &handlerutil.Out[types.ContainerDetails]{
			Body: types.ApiResponse[types.ContainerDetails]{
				Success: true,
				Data: types.ContainerDetails{
					ID:         newContainerID,
					ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
				},
			},
		}, nil
	}

	return &handlerutil.Out[types.ContainerDetails]{
		Body: types.ApiResponse[types.ContainerDetails]{
			Success: true,
			Data: types.ContainerDetails{ //exhaustruct:enforce
				ID:                details.ID,
				Name:              details.Name,
				Image:             details.Image,
				ImageID:           details.ImageID,
				Created:           details.Created,
				State:             details.State,
				Config:            details.Config,
				HostConfig:        details.HostConfig,
				NetworkSettings:   details.NetworkSettings,
				Ports:             details.Ports,
				Mounts:            details.Mounts,
				Labels:            details.Labels,
				ComposeInfo:       details.ComposeInfo,
				IconLightURL:      details.IconLightURL,
				IconDarkURL:       details.IconDarkURL,
				RedeployDisabled:  details.RedeployDisabled,
				AutoUpdateEnabled: details.AutoUpdateEnabled,
				UpdateInfo:        details.UpdateInfo,
				ActivityID:        mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

func (h *ContainerHandler) DeleteContainer(ctx context.Context, input *DeleteContainerInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		types.ActivityTypeContainerDelete,
		"container",
		input.ContainerID,
		h.containerActivityName(
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
		apiErr := common.ToAPIError(common.ClassifyDockerError(deleteContainerErr))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete container: "+apiErr.Message)
	}
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Container deleted", nil)

	return handlerutil.MessageOutput("Container deleted successfully", activityID), nil
}

func (h *ContainerHandler) SetAutoUpdate(ctx context.Context, input *SetAutoUpdateInput) (*handlerutil.Out[types.MessageResponse], error) {
	// Resolve container name from ID
	containerName, err := h.containerService.GetContainerNameByID(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error404NotFound("container not found")
	}

	if setContainerAutoUpdateExclusionErr := h.settingsService.SetContainerAutoUpdateExclusion(ctx, containerName, !input.Body.Enabled); setContainerAutoUpdateExclusionErr != nil {
		return nil, huma.Error500InternalServerError("failed to update auto-update setting")
	}

	return handlerutil.MessageOutput(kit.Ternary(input.Body.Enabled, "Auto-update enabled", "Auto-update disabled"), ""), nil
}
