package system

import (
	"cmp"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/system/children/backup"
	"getarcane.dev/arcane/v3/internal/system/children/prune"
	"getarcane.dev/arcane/v3/internal/system/children/upgrade"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service         *SystemService
		activityService *activity.ActivityService
		cfg             *config.Config
	}
)

func New(
	service *SystemService,
	activityService *activity.ActivityService,
	cfg *config.Config,
) *Module {
	return &Module{service: service, activityService: activityService, cfg: cfg}
}

func (m *Module) Service() *SystemService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		m = &Module{}
	}
	service := cmp.Or(m.service, &SystemService{})

	RegisterSystem(api, NewHandler(m.service, appCtx.Context()))
	prune.RegisterRoutes(api, prune.NewHandler(service.prune, appCtx.Context()))
	upgrade.RegisterRoutes(api, upgrade.NewHandler(service.upgrade, m.cfg, appCtx.Context()))
	backup.RegisterRoutes(api, backup.NewHandler(service.backup, m.activityService, appCtx.Context()))
}

func RegisterSystem(api huma.API, h *SystemHandler) {
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

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "system-containers-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/containers",
		Summary:     "Run bulk container action",
		Description: "Start all, start stopped, or stop all Docker containers",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"startall":     authz.PermContainersStart,
		"startstopped": authz.PermContainersStart,
		"stopall":      authz.PermContainersStop,
	}, h.RunContainersAction)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "convert-docker-run",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/system/convert",
		Summary:     "Convert docker run command",
		Description: "Convert a docker run command to docker-compose format",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersCreate, h.ConvertDockerRun)
}
