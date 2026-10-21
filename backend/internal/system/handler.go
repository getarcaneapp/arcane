package system

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/docker/convert"
	converttypes "go.getarcane.app/docker/convert/types"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// SystemHandler handles system management endpoints.
type SystemHandler struct {
	systemService *SystemService
	appCtx        context.Context
}

type SystemHealthInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetDockerInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetDockerInfoOutput struct {
	Body types.DockerInfo
}

type StartAllContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type StartAllStoppedContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type StopAllContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ConvertDockerRunInput struct {
	EnvironmentID string                        `path:"id" doc:"Environment ID"`
	Body          types.ConvertDockerRunRequest `doc:"Docker run command"`
}

type ConvertDockerRunOutput struct {
	Body types.ConvertDockerRunResponse
}

func NewHandler(systemService *SystemService, appCtx context.Context) *SystemHandler {
	return &SystemHandler{systemService: systemService, appCtx: appCtx}
}

// Health checks if the Docker daemon is responsive.
func (h *SystemHandler) Health(ctx context.Context, input *SystemHealthInput) (*struct{}, error) {
	if err := h.systemService.PingDocker(ctx); err != nil {
		return nil, huma.Error503ServiceUnavailable("Docker is not responsive: " + err.Error())
	}

	return nil, nil
}

// GetDockerInfo returns Docker daemon version and system information.
func (h *SystemHandler) GetDockerInfo(ctx context.Context, input *GetDockerInfoInput) (*GetDockerInfoOutput, error) {
	info, err := h.systemService.GetDockerInfo(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get Docker info: " + err.Error())
	}

	return &GetDockerInfoOutput{Body: info}, nil
}

// StartAllContainers starts all Docker containers.
func (h *SystemHandler) StartAllContainers(ctx context.Context, input *StartAllContainersInput) (*handlerutil.Out[types.ContainerActionResult], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StartAllContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to start containers: " + err.Error())
	}

	return &handlerutil.Out[types.ContainerActionResult]{
		Body: types.ApiResponse[types.ContainerActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllStoppedContainers starts all stopped Docker containers.
func (h *SystemHandler) StartAllStoppedContainers(ctx context.Context, input *StartAllStoppedContainersInput) (*handlerutil.Out[types.ContainerActionResult], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StartAllStoppedContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to start stopped containers: " + err.Error())
	}

	return &handlerutil.Out[types.ContainerActionResult]{
		Body: types.ApiResponse[types.ContainerActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StopAllContainers stops all running Docker containers.
func (h *SystemHandler) StopAllContainers(ctx context.Context, input *StopAllContainersInput) (*handlerutil.Out[types.ContainerActionResult], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StopAllContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to stop containers: " + err.Error())
	}

	return &handlerutil.Out[types.ContainerActionResult]{
		Body: types.ApiResponse[types.ContainerActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// ConvertDockerRun converts a docker run command to docker-compose format.
func (h *SystemHandler) ConvertDockerRun(ctx context.Context, input *ConvertDockerRunInput) (*ConvertDockerRunOutput, error) {
	result, err := convert.Convert(input.Body.DockerRunCommand, converttypes.Options{})
	if err != nil {
		if errors.Is(err, converttypes.ErrParse) {
			return nil, huma.Error400BadRequest("Failed to parse docker run command. Please check the syntax.")
		}
		return nil, huma.Error500InternalServerError("Failed to convert to Docker Compose format.")
	}

	return &ConvertDockerRunOutput{
		Body: types.ConvertDockerRunResponse{
			Success:       true,
			DockerCompose: string(result.YAML),
			EnvVars:       strings.TrimSuffix(string(result.EnvFile), "\n"),
			ServiceName: func() string {
				if len(result.Services) == 0 {
					return ""
				}
				return result.Services[0].Name
			}(),
		},
	}, nil
}
