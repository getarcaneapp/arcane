package system

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	dockersystem "github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/dockerinfo"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/docker/convert"
	converttypes "go.getarcane.app/docker/convert/types"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
)

// SystemHandler handles system management endpoints.
type SystemHandler struct {
	dockerService      *docker.DockerClientService
	systemService      *SystemService
	upgradeService     *SystemUpgradeService
	environmentService *environment.EnvironmentService
	activityService    *activity.ActivityService
	cfg                *config.Config
	appCtx             context.Context
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
	EnvironmentID string                         `path:"id" doc:"Environment ID"`
	Body          system.ConvertDockerRunRequest `doc:"Docker run command"`
}

type ConvertDockerRunOutput struct {
	Body system.ConvertDockerRunResponse
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

// TriggerUpgradeData reports the upgrade was accepted. UpToDate lets a client skip
// waiting for a restart: the upgrader still pulls, but when the environment already
// runs the newest image it finds nothing to swap in and no restart follows.
type TriggerUpgradeData struct {
	Message  string `json:"message" doc:"Response message"`
	UpToDate bool   `json:"upToDate" doc:"Environment already runs the newest image, so no restart is expected"`
}

type TriggerUpdateAllInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type UpdateAllStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// RegisterSystem registers system management endpoints using Huma.
// WebSocket statistics endpoints live in api/ws.
func RegisterSystem(api huma.API, dockerService *docker.DockerClientService, systemService *SystemService, upgradeService *SystemUpgradeService, environmentService *environment.EnvironmentService, cfg *config.Config, activityService *activity.ActivityService, appCtx handlerutil.ActivityAppContext) {
	h := &SystemHandler{
		dockerService:      dockerService,
		systemService:      systemService,
		upgradeService:     upgradeService,
		environmentService: environmentService,
		activityService:    activityService,
		cfg:                cfg,
		appCtx:             appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "system-health",
		Method:        http.MethodHead,
		Path:          "/environments/{id}/system/health",
		Summary:       "Check system health",
		Description:   "Check if the Docker daemon is responsive",
		Tags:          []string{"System"},
		DefaultStatus: http.StatusOK,
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.Health)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-docker-info",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/docker/info",
		Summary:     "Get Docker info",
		Description: "Get Docker daemon version and system information",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.GetDockerInfo)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-all",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/prune",
		Summary:     "Prune Docker resources",
		Description: "Remove unused Docker resources (containers, images, volumes, networks)",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemPrune, h.PruneAll)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "start-all-containers",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers/start-all",
		Summary:     "Start all containers",
		Description: "Start all Docker containers",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersStart, h.StartAllContainers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "start-all-stopped-containers",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers/start-stopped",
		Summary:     "Start all stopped containers",
		Description: "Start all stopped Docker containers",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersStart, h.StartAllStoppedContainers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "stop-all-containers",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers/stop-all",
		Summary:     "Stop all containers",
		Description: "Stop all running Docker containers",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersStop, h.StopAllContainers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "convert-docker-run",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/convert",
		Summary:     "Convert docker run command",
		Description: "Convert a docker run command to docker-compose format",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersCreate, h.ConvertDockerRun)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-upgrade",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/check",
		Summary:     "Check for system upgrade",
		Description: "Check if a system upgrade is available",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.CheckUpgradeAvailable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "trigger-upgrade",
		Method:        http.MethodPost,
		Path:          "/environments/{id}/system/upgrade",
		Summary:       "Trigger system upgrade",
		Description:   "Trigger a system upgrade",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"System"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemUpgrade, h.TriggerUpgrade)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "trigger-update-all",
		Method:        http.MethodPost,
		Path:          "/environments/{id}/system/upgrade/all",
		Summary:       "Update all environments",
		Description:   "Upgrade every Arcane environment, starting with the manager",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"System"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemUpgrade, h.TriggerUpdateAll)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-all-status",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/all/status",
		Summary:     "Get update-all status",
		Description: "Get the status of the latest update-all-environments job",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.GetUpdateAllStatus)
}

// rejectIfAgentModeInternal blocks manager-only operations when running as an agent.
func (h *SystemHandler) rejectIfAgentModeInternal() error {
	if h.cfg != nil && h.cfg.AgentMode {
		return huma.Error400BadRequest("update-all is managed on the Arcane manager")
	}
	return nil
}

// Health checks if the Docker daemon is responsive.
func (h *SystemHandler) Health(ctx context.Context, input *SystemHealthInput) (*SystemHealthOutput, error) {
	dockerClient, err := h.dockerService.GetClient(ctx)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable(errors.WithMessage(err, "Failed to connect to Docker").Error())
	}

	_, err = dockerClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		return nil, huma.Error503ServiceUnavailable(errors.WithMessage(err, "Docker is not responsive").Error())
	}

	return &SystemHealthOutput{}, nil
}

// GetDockerInfo returns Docker daemon version and system information.
func (h *SystemHandler) GetDockerInfo(ctx context.Context, input *GetDockerInfoInput) (*GetDockerInfoOutput, error) {
	dockerClient, err := h.dockerService.GetClient(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to connect to Docker").Error())
	}

	version, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get Docker version").Error())
	}

	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get Docker info").Error())
	}
	info := infoResult.Info

	cpuCount := info.NCPU
	memTotal := info.MemTotal

	// Apply cgroup limits only when running outside Docker (e.g. in LXC).
	// In Docker, --cpus/--memory are artificial operator constraints that
	// should not cap the host totals shown in the dashboard. The Docker
	// daemon's NCPU/MemTotal already reflect the real host. In LXC the
	// daemon may report the physical machine's full capacity while the
	// LXC guest has a smaller cgroup budget — apply those limits so the
	// dashboard shows what Arcane's host actually has available.
	if !cgroup.IsDockerContainer() {
		if cgroupLimits, err := cgroup.DetectLimits(); err == nil {
			if limit := cgroupLimits.MemoryLimit; limit > 0 {
				limitInt := limit
				if memTotal == 0 || limitInt < memTotal {
					memTotal = limitInt
				}
			}
			if cgroupLimits.CPUCount > 0 && (cpuCount == 0 || cgroupLimits.CPUCount < cpuCount) {
				cpuCount = cgroupLimits.CPUCount
			}
		}
	}

	info.NCPU = cpuCount
	info.MemTotal = memTotal

	gitCommit, goVersion, buildTime := extractVersionDetailsFromComponents(version.Components)

	return &GetDockerInfoOutput{
		Body: dockerinfo.Info{
			Success:    true,
			APIVersion: version.APIVersion,
			GitCommit:  gitCommit,
			GoVersion:  goVersion,
			Os:         version.Os,
			Arch:       version.Arch,
			BuildTime:  buildTime,
			Info:       info,
		},
	}, nil
}

func extractVersionDetailsFromComponents(components []dockersystem.ComponentVersion) (gitCommit, goVersion, buildTime string) {
	for _, component := range components {
		if component.Details == nil {
			continue
		}

		for key, value := range component.Details {
			switch strings.ToLower(key) {
			case "gitcommit":
				if gitCommit == "" {
					gitCommit = value
				}
			case "goversion":
				if goVersion == "" {
					goVersion = value
				}
			case "buildtime":
				if buildTime == "" {
					buildTime = value
				}
			}
		}
	}

	return gitCommit, goVersion, buildTime
}

// PruneAll removes unused Docker resources.
func (h *SystemHandler) PruneAll(ctx context.Context, input *PruneAllInput) (*handlerutil.Out[system.PruneAllResult], error) {
	slog.InfoContext(ctx, "System prune operation initiated",
		"containers", input.Body.Containers,
		"images", input.Body.Images,
		"volumes", input.Body.Volumes,
		"networks", input.Body.Networks,
		"build_cache", input.Body.BuildCache)

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result := h.systemService.StartPruneAll(runtimeCtx, input.EnvironmentID, input.Body)

	slog.InfoContext(runtimeCtx, "System prune background activity started", "activityId", result.ActivityID)

	return &handlerutil.Out[system.PruneAllResult]{
		Body: base.ApiResponse[system.PruneAllResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllContainers starts all Docker containers.
func (h *SystemHandler) StartAllContainers(ctx context.Context, input *StartAllContainersInput) (*handlerutil.Out[containertypes.ActionResult], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StartAllContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to start containers").Error())
	}

	return &handlerutil.Out[containertypes.ActionResult]{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllStoppedContainers starts all stopped Docker containers.
func (h *SystemHandler) StartAllStoppedContainers(ctx context.Context, input *StartAllStoppedContainersInput) (*handlerutil.Out[containertypes.ActionResult], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StartAllStoppedContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to start stopped containers").Error())
	}

	return &handlerutil.Out[containertypes.ActionResult]{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StopAllContainers stops all running Docker containers.
func (h *SystemHandler) StopAllContainers(ctx context.Context, input *StopAllContainersInput) (*handlerutil.Out[containertypes.ActionResult], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StopAllContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to stop containers").Error())
	}

	return &handlerutil.Out[containertypes.ActionResult]{
		Body: base.ApiResponse[containertypes.ActionResult]{
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

	serviceName := ""
	if len(result.Services) > 0 {
		serviceName = result.Services[0].Name
	}

	return &ConvertDockerRunOutput{
		Body: system.ConvertDockerRunResponse{
			Success:       true,
			DockerCompose: string(result.YAML),
			EnvVars:       strings.TrimSuffix(string(result.EnvFile), "\n"),
			ServiceName:   serviceName,
		},
	}, nil
}

// CheckUpgradeAvailable checks if a system upgrade is available.
func (h *SystemHandler) CheckUpgradeAvailable(ctx context.Context, input *CheckUpgradeInput) (*CheckUpgradeOutput, error) {
	canUpgrade, err := h.upgradeService.CanUpgrade(ctx)
	if err != nil {
		slog.Debug("System upgrade check failed", "error", err)
		return &CheckUpgradeOutput{
			Body: UpgradeCheckResultData{
				CanUpgrade: false,
				Error:      true,
				Message:    errors.WithMessage(err, "Failed to check for updates").Error(),
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
func (h *SystemHandler) TriggerUpgrade(ctx context.Context, input *TriggerUpgradeInput) (*handlerutil.Out[TriggerUpgradeData], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	slog.Info("System upgrade triggered", "user", user.Username, "userId", user.ID)

	// Resolved before triggering, while the version check still describes the running
	// container: the upgrade itself may replace it.
	upToDate := h.upgradeService.AlreadyOnNewestImage(ctx)

	_, err = h.upgradeService.TriggerUpgradeViaCLI(ctx, *user, updater.SelfUpdateTarget{})
	if err != nil {
		slog.Error("System upgrade failed", "error", err, "user", user.Username)

		if errors.Is(err, common.ErrUpgradeInProgress) {
			return nil, huma.Error409Conflict(errors.WithMessage(err, "Failed to initiate upgrade").Error())
		}

		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to initiate upgrade").Error())
	}

	message := "Upgrade initiated successfully. A new container is being created and will replace this one shortly."
	if upToDate {
		message = "Already running the newest image. The upgrade pulled it again and left the container in place."
	}

	return &handlerutil.Out[TriggerUpgradeData]{
		Body: base.ApiResponse[TriggerUpgradeData]{
			Success: true,
			Data: TriggerUpgradeData{
				Message:  message,
				UpToDate: upToDate,
			},
		},
	}, nil
}

// TriggerUpdateAll starts a fleet-wide update, upgrading the manager first and then
// the remote agents (the latter resume after the manager restarts).
func (h *SystemHandler) TriggerUpdateAll(ctx context.Context, input *TriggerUpdateAllInput) (*handlerutil.Out[EnvironmentUpdateJob], error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	slog.Info("Update-all environments triggered", "user", user.Username, "userId", user.ID)

	// Use a runtime context so the agents phase can outlive the request when the
	// manager is already up to date.
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)

	job, err := h.upgradeService.StartUpdateAll(runtimeCtx, *user, h.environmentService)
	if err != nil {
		if errors.Is(err, common.ErrUpdateAllInProgress) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to initiate upgrade").Error())
	}

	return &handlerutil.Out[EnvironmentUpdateJob]{
		Body: base.ApiResponse[EnvironmentUpdateJob]{
			Success: true,
			Data:    *job,
		},
	}, nil
}

// GetUpdateAllStatus returns the latest update-all job for live progress polling.
func (h *SystemHandler) GetUpdateAllStatus(ctx context.Context, input *UpdateAllStatusInput) (*handlerutil.Out[EnvironmentUpdateJob], error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}

	job, err := h.upgradeService.GetLatestUpdateAllJob(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if job == nil {
		return nil, huma.Error404NotFound("no update-all job found")
	}

	return &handlerutil.Out[EnvironmentUpdateJob]{
		Body: base.ApiResponse[EnvironmentUpdateJob]{
			Success: true,
			Data:    *job,
		},
	}, nil
}
