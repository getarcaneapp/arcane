// Package updater owns automatic container and project updates: deciding what
// is out of date, applying the update, and the HTTP surface that drives it.
package updater

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Dependencies are the collaborators the updater domain needs.
type Dependencies struct {
	DB           *database.DB
	Settings     *settings.SettingsService
	Docker       *docker.DockerClientService
	Project      *project.ProjectService
	ImageUpdate  *imageupdate.ImageUpdateService
	Registry     *registry.ContainerRegistryService
	Event        *event.EventService
	Image        *image.ImageService
	Notification *notification.NotificationService
	SelfUpgrade  selfUpgradeServiceInternal
	Activity     *activity.ActivityService
}

// Module wires the updater domain and mounts its routes.
type Module struct {
	service *UpdaterService
}

// New builds the updater domain from its dependencies.
func New(deps Dependencies) (*Module, error) {
	service, err := NewUpdaterService(
		deps.DB,
		deps.Settings,
		deps.Docker,
		deps.Project,
		deps.ImageUpdate,
		deps.Registry,
		deps.Event,
		deps.Image,
		deps.Notification,
		deps.SelfUpgrade,
		deps.Activity,
	)
	if err != nil {
		return nil, err
	}
	return &Module{service: service}, nil
}

// Service exposes the updater service to collaborators that trigger updates
// outside the HTTP surface, such as the auto-update job and webhooks.
func (m *Module) Service() *UpdaterService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the updater endpoints. A nil module still registers, so
// OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterUpdater(api, nil, appCtx)
		return
	}
	RegisterUpdater(api, m.service, appCtx)
}
