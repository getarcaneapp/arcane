// Package dashboard owns the dashboard snapshot: the service that assembles it
// from the container, project, image, volume and vulnerability domains, and the
// HTTP surface that serves it.
package dashboard

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
)

// Dependencies are the collaborators the dashboard domain needs.
type Dependencies struct {
	DB            *database.DB
	Docker        *docker.DockerClientService
	Container     *container.ContainerService
	Project       *project.ProjectService
	Image         *image.ImageService
	Settings      *settings.SettingsService
	Vulnerability *vulnerability.VulnerabilityService
	Environment   *environment.EnvironmentService
	Version       *version.VersionService
	Volume        *volume.VolumeService
}

// Module is the dashboard domain's wiring seam: it owns the service and the
// handler, and mounts the domain's routes.
type Module struct {
	service *DashboardService
	handler *DashboardHandler
}

// New builds the dashboard domain from its dependencies.
func New(deps Dependencies) *Module {
	service := NewDashboardService(
		deps.DB,
		deps.Docker,
		deps.Container,
		deps.Project,
		deps.Image,
		deps.Settings,
		deps.Vulnerability,
		deps.Environment,
		deps.Version,
		deps.Volume,
	)
	return &Module{service: service, handler: NewHandler(service, deps.Environment)}
}

// Handler exposes the dashboard stream producer.
func (m *Module) Handler() *DashboardHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

// Service exposes the dashboard service to collaborators that compose its
// producers, such as the multiplexed client stream.
func (m *Module) Service() *DashboardService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the dashboard endpoints. A nil module still registers,
// so OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterDashboard(api, nil, nil)
		return
	}
	RegisterDashboard(api, m.service, m.handler.environmentService)
}
