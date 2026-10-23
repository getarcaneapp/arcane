// Package container owns Docker container lifecycle, inspection, listing and
// stats, plus the HTTP surface that exposes them.
package container

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/tracing"
)

// Module wires the container domain and mounts its routes.
type Module struct {
	service         *ContainerService
	dockerService   *docker.DockerClientService
	settingsService *settings.SettingsService
	activityService *activity.ActivityService
}

// New wires container routes around an existing service and registers per-container resource metrics once.
func New(service *ContainerService, dockerService *docker.DockerClientService, settingsService *settings.SettingsService, activityService *activity.ActivityService) *Module {
	if err := service.stats.ObserveResources(otel.Meter(tracing.InstrumentationName), service.dockerService.ListContainers); err != nil {
		otel.Handle(err)
	}
	return &Module{
		service:         service,
		dockerService:   dockerService,
		settingsService: settingsService,
		activityService: activityService,
	}
}

// Service exposes the container service to collaborators.
func (m *Module) Service() *ContainerService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the container endpoints. A nil module still registers,
// so OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterContainers(api, nil, nil, nil, nil, appCtx)
		return
	}
	RegisterContainers(api, m.service, m.dockerService, m.settingsService, m.activityService, appCtx)
}

func RegisterContainers(
	api huma.API,
	containerSvc *ContainerService,
	dockerSvc *docker.DockerClientService,
	settingsSvc *settings.SettingsService,
	activitySvc *activity.ActivityService,
	appCtx handlerutil.ActivityAppContext,
) {
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
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermContainersList, h.ListContainers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "container-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/counts",
		Summary:     "Container status counts",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
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
		OperationID: "get-container-processes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}/processes",
		Summary:     "Get container processes",
		Description: "Snapshot of the processes running inside the container, as reported by Docker",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRead, h.GetContainerProcesses)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "container-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}",
		Summary:     "Run container lifecycle action",
		Description: "Start, stop, restart, kill, pause, or unpause a container. Kill accepts an optional signal (default SIGKILL).",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"start":   authz.PermContainersStart,
		"stop":    authz.PermContainersStop,
		"restart": authz.PermContainersRestart,
		"kill":    authz.PermContainersKill,
		"pause":   authz.PermContainersPause,
		"unpause": authz.PermContainersPause,
	}, h.RunContainerAction)

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
		Path:        "/environments/{id}/containers/compose",
		Summary:     "Generate compose file",
		Description: "Generate a compose file from existing containers",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRead, h.GenerateCompose)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-container-edit-config",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}/config",
		Summary:     "Get container edit config",
		Description: "Editable configuration snapshot backing the container edit form",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersRead, h.GetContainerEditConfig)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "edit-container",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/containers/{containerId}/config",
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
		OperationID: "download-container-logs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/containers/{containerId}/logs/download",
		Summary:     "Download container logs",
		Description: "Download every log line Docker retains for the container as a text file",
		Tags:        []string{"Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersLogs, h.DownloadContainerLogs)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "set-container-auto-update",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/containers/{containerId}/autoupdate",
		Summary:     "Set container auto-update",
		Description: "Enable or disable auto-update for a specific container",
		Tags:        []string{"Containers", "Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersAutoUpdate, h.SetAutoUpdate)
}
