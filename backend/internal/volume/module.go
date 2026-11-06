package volume

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/internal/volume/children/backup"
	"getarcane.dev/arcane/v3/internal/volume/children/workspace"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service            *VolumeService
		dockerService      *docker.DockerClientService
		activityService    *activity.ActivityService
		environmentService *environment.EnvironmentService
		uploadService      *upload.UploadService
	}

	Params struct {
		fx.In

		Service            *VolumeService
		DockerService      *docker.DockerClientService
		ActivityService    *activity.ActivityService
		EnvironmentService *environment.EnvironmentService
		UploadService      *upload.UploadService
	}
)

func New(p Params) *Module {
	return &Module{service: p.Service, dockerService: p.DockerService, activityService: p.ActivityService, environmentService: p.EnvironmentService, uploadService: p.UploadService}
}

func (m *Module) Service() *VolumeService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		m = &Module{}
	}
	service := m.service
	if service == nil {
		service = &VolumeService{}
	}

	RegisterVolumes(api, m.dockerService, m.service, m.activityService, m.environmentService, appCtx)
	workspace.RegisterRoutes(api, workspace.NewHandler(service.workspace, m.activityService, appCtx.Context()))
	backup.RegisterRoutes(api, backup.NewHandler(backup.HandlerParams{
		Service:            service.backup,
		ActivityService:    m.activityService,
		EnvironmentService: m.environmentService,
		UploadService:      m.uploadService,
		AppCtx:             appCtx.Context(),
	}))
}

func RegisterVolumes(
	api huma.API,
	dockerService *docker.DockerClientService,
	volumeService *VolumeService,
	activityService *activity.ActivityService,
	environmentService *environment.EnvironmentService,
	appCtx handlerutil.ActivityAppContext,
) {
	h := &VolumeHandler{
		volumeService:      volumeService,
		dockerService:      dockerService,
		activityService:    activityService,
		environmentService: environmentService,
		appCtx:             appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-usage-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/counts",
		Summary:     "Get volume usage counts",
		Description: "Get counts of volumes in use, unused, and total",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermVolumesList, h.GetVolumeUsageCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-volumes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes",
		Summary:     "List volumes",
		Description: "Get a paginated list of Docker volumes",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermVolumesList, h.ListVolumes)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}",
		Summary:     "Get volume by name",
		Description: "Get a Docker volume by its name",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.GetVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-volume",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes",
		Summary:     "Create a volume",
		Description: "Create a new Docker volume",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesCreate, h.CreateVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "remove-volume",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/volumes/{volumeName}",
		Summary:     "Remove a volume",
		Description: "Remove a Docker volume by name",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesDelete, h.RemoveVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "rename-volume",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/{volumeName}/rename",
		Summary:     "Rename a volume",
		Description: "Copy an unused Docker volume to a new name and remove the source",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRename, h.RenameVolume)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-volumes",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/volumes/prune",
		Summary:     "Prune unused volumes",
		Description: "Remove all unused Docker volumes",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesPrune, h.PruneVolumes)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-usage",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/{volumeName}/usage",
		Summary:     "Get volume usage",
		Description: "Get containers using a specific volume",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVolumesRead, h.GetVolumeUsage)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-volume-sizes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/volumes/sizes",
		Summary:     "Get volume sizes",
		Description: "Get disk usage sizes for all volumes (slow operation)",
		Tags:        []string{"Volumes"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermVolumesList, h.GetVolumeSizes)
}
