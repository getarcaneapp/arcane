// Package system owns Docker system-wide operations — prune, disk usage, host
// info, self-upgrade, system backups — and the HTTP surface that exposes them.
package system

import (
	"cmp"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system/children/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system/children/prune"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system/children/upgrade"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Module wires the system domain and mounts its routes.
type Module struct {
	service         *SystemService
	activityService *activity.ActivityService
	cfg             *config.Config
}

// New wires system routes around an existing service.
func New(
	service *SystemService,
	activityService *activity.ActivityService,
	cfg *config.Config,
) *Module {
	return &Module{service: service, activityService: activityService, cfg: cfg}
}

// Service exposes the system service to collaborators.
func (m *Module) Service() *SystemService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the system endpoints. A nil module still registers, so
// OpenAPI spec generation can discover the routes without a service graph.
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

// RegisterSystem registers system management endpoints using Huma.
// WebSocket statistics endpoints live in api/ws.
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
