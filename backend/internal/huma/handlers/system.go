package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/types/base"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/getarcaneapp/arcane/types/dockerinfo"
	"github.com/getarcaneapp/arcane/types/system"
)

// SystemHandler handles system management endpoints.
type SystemHandler struct {
	dockerService  *services.DockerClientService
	systemService  *services.SystemService
	upgradeService *services.SystemUpgradeService
	cfg            *config.Config
}

// --- Input/Output Types ---

type SystemHealthInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type SystemHealthOutput struct {
	Status int `status:"200"`
}

type GetDockerInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetDockerInfoOutput struct {
	Body dockerinfo.Info
}

type PruneAllInput struct {
	EnvironmentID string                 `path:"id" doc:"Environment ID"`
	Body          system.PruneAllRequest `doc:"Prune options"`
}

type PruneAllOutput struct {
	Body base.ApiResponse[system.PruneAllResult]
}

type StartAllContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type StartAllContainersOutput struct {
	Body base.ApiResponse[containertypes.ActionResult]
}

type StartAllStoppedContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type StartAllStoppedContainersOutput struct {
	Body base.ApiResponse[containertypes.ActionResult]
}

type StopAllContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type StopAllContainersOutput struct {
	Body base.ApiResponse[containertypes.ActionResult]
}

type ConvertDockerRunInput struct {
	EnvironmentID string                         `path:"id" doc:"Environment ID"`
	Body          models.ConvertDockerRunRequest `doc:"Docker run command"`
}

type ConvertDockerRunOutput struct {
	Body models.ConvertDockerRunResponse
}

type CheckUpgradeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// UpgradeCheckResultData is the response for upgrade check.
type UpgradeCheckResultData struct {
	CanUpgrade bool   `json:"canUpgrade"`
	Error      bool   `json:"error"`
	Message    string `json:"message"`
}

type CheckUpgradeOutput struct {
	Body UpgradeCheckResultData
}

type TriggerUpgradeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type TriggerUpgradeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// RegisterSystem registers system management endpoints using Huma.
// Note: WebSocket endpoints (stats) remain in the Gin handler.
func RegisterSystem(api huma.API, dockerService *services.DockerClientService, systemService *services.SystemService, upgradeService *services.SystemUpgradeService, cfg *config.Config) {
	h := &SystemHandler{
		dockerService:  dockerService,
		systemService:  systemService,
		upgradeService: upgradeService,
		cfg:            cfg,
	}

	huma.Register(api, huma.Operation{
		OperationID:   "system-health",
		Method:        http.MethodHead,
		Path:          "/environments/{id}/system/health",
		Summary:       "Check system health",
		Description:   "Check if the Docker daemon is responsive",
		Tags:          []string{"System"},
		DefaultStatus: http.StatusOK,
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.Health)

	huma.Register(api, huma.Operation{
		OperationID: "get-docker-info",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/docker/info",
		Summary:     "Get Docker info",
		Description: "Get Docker daemon version and system information",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetDockerInfo)

	huma.Register(api, huma.Operation{
		OperationID: "prune-all",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/prune",
		Summary:     "Prune Docker resources",
		Description: "Remove unused Docker resources (containers, images, volumes, networks)",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.PruneAll)

	huma.Register(api, huma.Operation{
		OperationID: "start-all-containers",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers/start-all",
		Summary:     "Start all containers",
		Description: "Start all Docker containers",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.StartAllContainers)

	huma.Register(api, huma.Operation{
		OperationID: "start-all-stopped-containers",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers/start-stopped",
		Summary:     "Start all stopped containers",
		Description: "Start all stopped Docker containers",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.StartAllStoppedContainers)

	huma.Register(api, huma.Operation{
		OperationID: "stop-all-containers",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers/stop-all",
		Summary:     "Stop all containers",
		Description: "Stop all running Docker containers",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.StopAllContainers)

	huma.Register(api, huma.Operation{
		OperationID: "convert-docker-run",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/convert",
		Summary:     "Convert docker run command",
		Description: "Convert a docker run command to docker-compose format",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ConvertDockerRun)

	huma.Register(api, huma.Operation{
		OperationID: "check-upgrade",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/check",
		Summary:     "Check for system upgrade",
		Description: "Check if a system upgrade is available",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CheckUpgradeAvailable)

	huma.Register(api, huma.Operation{
		OperationID:   "trigger-upgrade",
		Method:        http.MethodPost,
		Path:          "/environments/{id}/system/upgrade",
		Summary:       "Trigger system upgrade",
		Description:   "Trigger a system upgrade",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.TriggerUpgrade)
}

// Health checks if the Docker daemon is responsive.
func (h *SystemHandler) Health(ctx context.Context, input *SystemHealthInput) (*SystemHealthOutput, error) {
	if h.dockerService == nil {
		return nil, huma.Error503ServiceUnavailable("docker service not available")
	}

	dockerClient, err := h.dockerService.GetClient()
	if err != nil {
		return nil, huma.Error503ServiceUnavailable((&common.DockerConnectionError{Err: err}).Error())
	}

	_, err = dockerClient.Ping(ctx)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable((&common.DockerPingError{Err: err}).Error())
	}

	return &SystemHealthOutput{}, nil
}

// GetDockerInfo returns Docker daemon version and system information.
func (h *SystemHandler) GetDockerInfo(ctx context.Context, input *GetDockerInfoInput) (*GetDockerInfoOutput, error) {
	if h.dockerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	dockerClient, err := h.dockerService.GetClient()
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerConnectionError{Err: err}).Error())
	}

	version, err := dockerClient.ServerVersion(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerVersionError{Err: err}).Error())
	}

	info, err := dockerClient.Info(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerInfoError{Err: err}).Error())
	}

	cpuCount := info.NCPU
	memTotal := info.MemTotal

	// Check for cgroup limits (LXC, Docker, etc.)
	if cgroupLimits, err := docker.DetectCgroupLimits(); err == nil {
		if limit := cgroupLimits.MemoryLimit; limit > 0 {
			limitInt := int64(limit)
			if memTotal == 0 || limitInt < memTotal {
				memTotal = limitInt
			}
		}
		if cgroupLimits.CPUCount > 0 && (cpuCount == 0 || cgroupLimits.CPUCount < cpuCount) {
			cpuCount = cgroupLimits.CPUCount
		}
	}

	info.NCPU = cpuCount
	info.MemTotal = memTotal

	return &GetDockerInfoOutput{
		Body: dockerinfo.Info{
			Success:    true,
			APIVersion: version.APIVersion,
			GitCommit:  version.GitCommit,
			GoVersion:  version.GoVersion,
			Os:         version.Os,
			Arch:       version.Arch,
			BuildTime:  version.BuildTime,
			Info:       info,
		},
	}, nil
}

// PruneAll removes unused Docker resources.
func (h *SystemHandler) PruneAll(ctx context.Context, input *PruneAllInput) (*PruneAllOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "System prune operation initiated",
		"containers", input.Body.Containers,
		"images", input.Body.Images,
		"volumes", input.Body.Volumes,
		"networks", input.Body.Networks,
		"build_cache", input.Body.BuildCache,
		"dangling", input.Body.Dangling)

	result, err := h.systemService.PruneAll(ctx, input.Body)
	if err != nil {
		slog.ErrorContext(ctx, "System prune operation failed", "error", err)
		return nil, huma.Error500InternalServerError((&common.SystemPruneError{Err: err}).Error())
	}

	slog.InfoContext(ctx, "System prune operation completed successfully",
		"containers_pruned", len(result.ContainersPruned),
		"images_deleted", len(result.ImagesDeleted),
		"volumes_deleted", len(result.VolumesDeleted),
		"networks_deleted", len(result.NetworksDeleted),
		"space_reclaimed", result.SpaceReclaimed)

	return &PruneAllOutput{
		Body: base.ApiResponse[system.PruneAllResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllContainers starts all Docker containers.
func (h *SystemHandler) StartAllContainers(ctx context.Context, input *StartAllContainersInput) (*StartAllContainersOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	result, err := h.systemService.StartAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStartAllError{Err: err}).Error())
	}

	return &StartAllContainersOutput{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllStoppedContainers starts all stopped Docker containers.
func (h *SystemHandler) StartAllStoppedContainers(ctx context.Context, input *StartAllStoppedContainersInput) (*StartAllStoppedContainersOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	result, err := h.systemService.StartAllStoppedContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStartStoppedError{Err: err}).Error())
	}

	return &StartAllStoppedContainersOutput{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StopAllContainers stops all running Docker containers.
func (h *SystemHandler) StopAllContainers(ctx context.Context, input *StopAllContainersInput) (*StopAllContainersOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	result, err := h.systemService.StopAllContainers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStopAllError{Err: err}).Error())
	}

	return &StopAllContainersOutput{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// ConvertDockerRun converts a docker run command to docker-compose format.
func (h *SystemHandler) ConvertDockerRun(ctx context.Context, input *ConvertDockerRunInput) (*ConvertDockerRunOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	parsed, err := h.systemService.ParseDockerRunCommand(input.Body.DockerRunCommand)
	if err != nil {
		return nil, huma.Error400BadRequest((&common.DockerRunParseError{Err: err}).Error())
	}

	dockerCompose, envVars, serviceName, err := h.systemService.ConvertToDockerCompose(parsed)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerComposeConversionError{Err: err}).Error())
	}

	return &ConvertDockerRunOutput{
		Body: models.ConvertDockerRunResponse{
			Success:       true,
			DockerCompose: dockerCompose,
			EnvVars:       envVars,
			ServiceName:   serviceName,
		},
	}, nil
}

// CheckUpgradeAvailable checks if a system upgrade is available.
func (h *SystemHandler) CheckUpgradeAvailable(ctx context.Context, input *CheckUpgradeInput) (*CheckUpgradeOutput, error) {
	if h.upgradeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	canUpgrade, err := h.upgradeService.CanUpgrade(ctx)
	if err != nil {
		slog.Debug("System upgrade check failed", "error", err)
		return &CheckUpgradeOutput{
			Body: UpgradeCheckResultData{
				CanUpgrade: false,
				Error:      true,
				Message:    (&common.UpgradeCheckError{Err: err}).Error(),
			},
		}, nil
	}

	return &CheckUpgradeOutput{
		Body: UpgradeCheckResultData{
			CanUpgrade: canUpgrade,
			Error:      false,
			Message:    "System can be upgraded",
		},
	}, nil
}

// TriggerUpgrade triggers a system upgrade.
func (h *SystemHandler) TriggerUpgrade(ctx context.Context, input *TriggerUpgradeInput) (*TriggerUpgradeOutput, error) {
	if h.upgradeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	slog.Info("System upgrade triggered", "user", user.Username, "userId", user.ID)

	err := h.upgradeService.TriggerUpgradeViaCLI(ctx, *user)
	if err != nil {
		slog.Error("System upgrade failed", "error", err, "user", user.Username)

		if errors.Is(err, services.ErrUpgradeInProgress) {
			return nil, huma.Error409Conflict((&common.UpgradeTriggerError{Err: err}).Error())
		}

		return nil, huma.Error500InternalServerError((&common.UpgradeTriggerError{Err: err}).Error())
	}

	return &TriggerUpgradeOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Upgrade initiated successfully. A new container is being created and will replace this one shortly.",
			},
		},
	}, nil
}
