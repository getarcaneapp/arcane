// Package system owns Docker system-wide operations — prune, disk usage, host
// info — and the HTTP surface that exposes them.
package system

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Module wires the system domain and mounts its routes.
type Module struct {
	service            *SystemService
	dockerService      *docker.DockerClientService
	upgradeService     *SystemUpgradeService
	environmentService *environment.EnvironmentService
	cfg                *config.Config
	activityService    *activity.ActivityService
}

// New wires system routes around an existing service.
func New(service *SystemService, dockerService *docker.DockerClientService, upgradeService *SystemUpgradeService, environmentService *environment.EnvironmentService, cfg *config.Config, activityService *activity.ActivityService) *Module {
	return &Module{service: service, dockerService: dockerService, upgradeService: upgradeService, environmentService: environmentService, cfg: cfg, activityService: activityService}
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
		RegisterSystem(api, nil, nil, nil, nil, nil, nil, appCtx)
		return
	}
	RegisterSystem(api, m.dockerService, m.service, m.upgradeService, m.environmentService, m.cfg, m.activityService, appCtx)
}
