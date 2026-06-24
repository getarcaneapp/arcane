package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/dockerinfo"
	"github.com/getarcaneapp/arcane/types/v2/system"
	dockersystem "github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	updatertypes "go.getarcane.app/updater/types"
)

// systemHandler handles system management endpoints.
type systemHandler struct {
	dockerService      *services.DockerClientService
	systemService      *services.SystemService
	upgradeService     *services.SystemUpgradeService
	environmentService *services.EnvironmentService
	activityService    *services.ActivityService
	cfg                *config.Config
	appCtx             context.Context
}

// --- Input/Output Types ---

type systemHealthInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type systemHealthOutput struct {
	Status int `status:"200"`
}

type getDockerInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type getDockerInfoOutput struct {
	Body dockerinfo.Info
}

type pruneAllInput struct {
	EnvironmentID string                 `path:"id" doc:"Environment ID"`
	Body          system.PruneAllRequest `doc:"Prune options"`
}

type pruneAllOutput struct {
	Body base.ApiResponse[system.PruneAllResult]
}

type startAllContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type startAllContainersOutput struct {
	Body base.ApiResponse[containertypes.ActionResult]
}

type startAllStoppedContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type startAllStoppedContainersOutput struct {
	Body base.ApiResponse[containertypes.ActionResult]
}

type stopAllContainersInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type stopAllContainersOutput struct {
	Body base.ApiResponse[containertypes.ActionResult]
}

type convertDockerRunInput struct {
	EnvironmentID string                         `path:"id" doc:"Environment ID"`
	Body          system.ConvertDockerRunRequest `doc:"Docker run command"`
}

type convertDockerRunOutput struct {
	Body system.ConvertDockerRunResponse
}

type checkUpgradeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// upgradeCheckResultData is the response for upgrade check.
type upgradeCheckResultData struct {
	CanUpgrade bool   `json:"canUpgrade"`
	Error      bool   `json:"error"`
	Message    string `json:"message"`
}

type checkUpgradeOutput struct {
	Body upgradeCheckResultData
}

type triggerUpgradeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type triggerUpgradeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type triggerUpdateAllInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type triggerUpdateAllOutput struct {
	Body base.ApiResponse[models.EnvironmentUpdateJob]
}

type updateAllStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type updateAllStatusOutput struct {
	Body base.ApiResponse[models.EnvironmentUpdateJob]
}

// RegisterSystem registers system management endpoints using Huma.
// Note: WebSocket endpoints (stats) remain in the Gin handler.
func RegisterSystem(api huma.API, dockerService *services.DockerClientService, systemService *services.SystemService, upgradeService *services.SystemUpgradeService, environmentService *services.EnvironmentService, cfg *config.Config, activityService *services.ActivityService, appCtx ActivityAppContext) {
	h := &systemHandler{
		dockerService:      dockerService,
		systemService:      systemService,
		upgradeService:     upgradeService,
		environmentService: environmentService,
		activityService:    activityService,
		cfg:                cfg,
		appCtx:             appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSystemRead, h.healthInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSystemRead, h.getDockerInfoInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSystemPrune, h.pruneAllInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermContainersStart, h.startAllContainersInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermContainersStart, h.startAllStoppedContainersInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermContainersStop, h.stopAllContainersInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermContainersCreate, h.convertDockerRunInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSystemRead, h.checkUpgradeAvailableInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSystemUpgrade, h.triggerUpgradeInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID:   "trigger-update-all",
		Method:        http.MethodPost,
		Path:          "/environments/{id}/system/upgrade/all",
		Summary:       "Update all environments",
		Description:   "Upgrade every Arcane environment, starting with the manager",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermSystemUpgrade, h.triggerUpdateAllInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-all-status",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/all/status",
		Summary:     "Get update-all status",
		Description: "Get the status of the latest update-all-environments job",
		Tags:        []string{"System"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermSystemRead, h.getUpdateAllStatusInternal)
}

// rejectIfAgentModeInternal blocks manager-only operations when running as an agent.
func (h *systemHandler) rejectIfAgentModeInternal() error {
	if h.cfg != nil && h.cfg.AgentMode {
		return huma.Error400BadRequest("update-all is managed on the Arcane manager")
	}
	return nil
}

// Health checks if the Docker daemon is responsive.
func (h *systemHandler) healthInternal(ctx context.Context, _ *systemHealthInput) (*systemHealthOutput, error) {
	if h.dockerService == nil {
		return nil, huma.Error503ServiceUnavailable("docker service not available")
	}

	dockerClient, err := h.dockerService.GetClient(ctx)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable((&common.DockerConnectionError{Err: err}).Error())
	}

	_, err = dockerClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		return nil, huma.Error503ServiceUnavailable((&common.DockerPingError{Err: err}).Error())
	}

	return &systemHealthOutput{}, nil
}

// GetDockerInfo returns Docker daemon version and system information.
func (h *systemHandler) getDockerInfoInternal(ctx context.Context, _ *getDockerInfoInput) (*getDockerInfoOutput, error) {
	if h.dockerService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	dockerClient, err := h.dockerService.GetClient(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerConnectionError{Err: err}).Error())
	}

	version, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerVersionError{Err: err}).Error())
	}

	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.DockerInfoError{Err: err}).Error())
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
	if !docker.IsDockerContainer() {
		if cgroupLimits, err := docker.DetectCgroupLimits(); err == nil {
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

	return &getDockerInfoOutput{
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
func (h *systemHandler) pruneAllInternal(ctx context.Context, input *pruneAllInput) (*pruneAllOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	slog.InfoContext(ctx, "System prune operation initiated",
		"containers", input.Body.Containers,
		"images", input.Body.Images,
		"volumes", input.Body.Volumes,
		"networks", input.Body.Networks,
		"build_cache", input.Body.BuildCache)

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result := h.systemService.StartPruneAll(runtimeCtx, input.EnvironmentID, input.Body)

	slog.InfoContext(runtimeCtx, "System prune background activity started", "activityId", result.ActivityID)

	return &pruneAllOutput{
		Body: base.ApiResponse[system.PruneAllResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllContainers starts all Docker containers.
func (h *systemHandler) startAllContainersInternal(ctx context.Context, input *startAllContainersInput) (*startAllContainersOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StartAllContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStartAllError{Err: err}).Error())
	}

	return &startAllContainersOutput{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StartAllStoppedContainers starts all stopped Docker containers.
func (h *systemHandler) startAllStoppedContainersInternal(ctx context.Context, input *startAllStoppedContainersInput) (*startAllStoppedContainersOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StartAllStoppedContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStartStoppedError{Err: err}).Error())
	}

	return &startAllStoppedContainersOutput{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// StopAllContainers stops all running Docker containers.
func (h *systemHandler) stopAllContainersInternal(ctx context.Context, input *stopAllContainersInput) (*stopAllContainersOutput, error) {
	if h.systemService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result, err := h.systemService.StopAllContainers(runtimeCtx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ContainerStopAllError{Err: err}).Error())
	}

	return &stopAllContainersOutput{
		Body: base.ApiResponse[containertypes.ActionResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// ConvertDockerRun converts a docker run command to docker-compose format.
func (h *systemHandler) convertDockerRunInternal(_ context.Context, input *convertDockerRunInput) (*convertDockerRunOutput, error) {
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

	return &convertDockerRunOutput{
		Body: system.ConvertDockerRunResponse{
			Success:       true,
			DockerCompose: dockerCompose,
			EnvVars:       envVars,
			ServiceName:   serviceName,
		},
	}, nil
}

// CheckUpgradeAvailable checks if a system upgrade is available.
func (h *systemHandler) checkUpgradeAvailableInternal(ctx context.Context, _ *checkUpgradeInput) (*checkUpgradeOutput, error) {
	if h.upgradeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	canUpgrade, err := h.upgradeService.CanUpgrade(ctx)
	if err != nil {
		slog.Debug("System upgrade check failed", "error", err)
		return &checkUpgradeOutput{
			Body: upgradeCheckResultData{
				CanUpgrade: false,
				Error:      true,
				Message:    (&common.UpgradeCheckError{Err: err}).Error(),
			},
		}, nil
	}

	return &checkUpgradeOutput{
		Body: upgradeCheckResultData{
			CanUpgrade: canUpgrade,
			Error:      false,
			Message:    "System can be upgraded",
		},
	}, nil
}

// TriggerUpgrade triggers a system upgrade.
func (h *systemHandler) triggerUpgradeInternal(ctx context.Context, _ *triggerUpgradeInput) (*triggerUpgradeOutput, error) {
	if h.upgradeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	slog.Info("System upgrade triggered", "user", user.Username, "userId", user.ID)

	err = h.upgradeService.TriggerUpgradeViaCLI(ctx, *user, updatertypes.SelfUpdateTarget{})
	if err != nil {
		slog.Error("System upgrade failed", "error", err, "user", user.Username)

		if common.IsUpgradeInProgressError(err) {
			return nil, huma.Error409Conflict((&common.UpgradeTriggerError{Err: err}).Error())
		}

		return nil, huma.Error500InternalServerError((&common.UpgradeTriggerError{Err: err}).Error())
	}

	return &triggerUpgradeOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Upgrade initiated successfully. A new container is being created and will replace this one shortly.",
			},
		},
	}, nil
}

// TriggerUpdateAll starts a fleet-wide update, upgrading the manager first and then
// the remote agents (the latter resume after the manager restarts).
func (h *systemHandler) triggerUpdateAllInternal(ctx context.Context, _ *triggerUpdateAllInput) (*triggerUpdateAllOutput, error) {
	if h.upgradeService == nil || h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	slog.Info("Update-all environments triggered", "user", user.Username, "userId", user.ID)

	// Use a runtime context so the agents phase can outlive the request when the
	// manager is already up to date.
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)

	job, err := h.upgradeService.StartUpdateAll(runtimeCtx, *user, h.environmentService)
	if err != nil {
		if common.IsUpdateAllInProgressError(err) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.UpgradeTriggerError{Err: err}).Error())
	}

	return &triggerUpdateAllOutput{
		Body: base.ApiResponse[models.EnvironmentUpdateJob]{
			Success: true,
			Data:    *job,
		},
	}, nil
}

// GetUpdateAllStatus returns the latest update-all job for live progress polling.
func (h *systemHandler) getUpdateAllStatusInternal(ctx context.Context, _ *updateAllStatusInput) (*updateAllStatusOutput, error) {
	if h.upgradeService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
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

	return &updateAllStatusOutput{
		Body: base.ApiResponse[models.EnvironmentUpdateJob]{
			Success: true,
			Data:    *job,
		},
	}, nil
}
