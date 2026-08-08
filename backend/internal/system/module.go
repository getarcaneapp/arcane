// Package system owns Docker system-wide operations — prune, disk usage, host
// info — and the HTTP surface that exposes them.
package system

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Dependencies are the collaborators the system domain needs.
type Dependencies struct {
	DB            *database.DB
	Config        *config.Config
	Docker        *docker.DockerClientService
	Container     *container.ContainerService
	Image         *image.ImageService
	Volume        *volume.VolumeService
	Network       *network.NetworkService
	Settings      *settings.SettingsService
	Activity      *activity.ActivityService
	SystemUpgrade *SystemUpgradeService
	Environment   *environment.EnvironmentService
}

// Module wires the system domain and mounts its routes.
type Module struct {
	service *SystemService
	deps    Dependencies
}

// New builds the system domain from its dependencies.
func New(deps Dependencies) *Module {
	return &Module{
		service: NewSystemService(
			deps.DB,
			deps.Docker,
			deps.Container,
			deps.Image,
			deps.Volume,
			deps.Network,
			deps.Settings,
			deps.Activity,
		),
		deps: deps,
	}
}

// Service exposes the system service to collaborators that need it directly,
// such as the scheduled prune job.
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
	RegisterSystem(api, m.deps.Docker, m.service, m.deps.SystemUpgrade, m.deps.Environment, m.deps.Config, m.deps.Activity, appCtx)
}
