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

type ContainersActionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Action        string `query:"action" required:"true" enum:"startall,startstopped,stopall" doc:"Bulk container action"`
}

type containersAction struct {
	Run   func(context.Context, string) (*types.ContainerActionResult, error)
	Error string
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

func (h *SystemHandler) RunContainersAction(ctx context.Context, input *ContainersActionInput) (*handlerutil.Out[types.ContainerActionResult], error) {
	action := map[string]containersAction{
		"startall": {
			Run:   h.systemService.StartAllContainers,
			Error: "Failed to start containers: ",
		},
		"startstopped": {
			Run:   h.systemService.StartAllStoppedContainers,
			Error: "Failed to start stopped containers: ",
		},
		"stopall": {
			Run:   h.systemService.StopAllContainers,
			Error: "Failed to stop containers: ",
		},
	}[input.Action]
	result, err := action.Run(utils.ActivityRuntimeContext(ctx, h.appCtx), input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(action.Error + err.Error())
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
