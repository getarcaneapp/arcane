// Package volume owns Docker volumes: CRUD and pruning, the helper-container
// file browser, backup and restore, and the HTTP surface for all of it.
package volume

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Dependencies are the collaborators the volume domain needs.
type Dependencies struct {
	DB          *database.DB
	Docker      *docker.DockerClientService
	Event       *event.EventService
	Settings    *settings.SettingsService
	Image       *image.ImageService
	Activity    *activity.ActivityService
	Environment *environment.EnvironmentService
	Container   *container.ContainerService
	Engine      *backup.Engine
	S3          *s3domain.S3DestinationService
	Config      *config.Config
	Upload      *upload.UploadService
}

// Module wires the volume domain and mounts its routes.
type Module struct {
	service *VolumeService
	deps    Dependencies
}

// New builds the volume domain from its dependencies.
func New(deps Dependencies) *Module {
	return &Module{
		service: NewVolumeService(deps.DB, deps.Docker, deps.Event, deps.Activity, deps.Settings, deps.Container, deps.Image, deps.Engine, deps.S3, deps.Config),
		deps:    deps,
	}
}

// Service exposes the volume service to collaborators that use it directly,
// such as the helper-container reaper job.
func (m *Module) Service() *VolumeService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the volume endpoints. A nil module still registers, so
// OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterVolumes(api, nil, nil, nil, nil, nil, appCtx)
		return
	}
	RegisterVolumes(api, m.deps.Docker, m.service, m.deps.Activity, m.deps.Environment, m.deps.Upload, appCtx)
}
