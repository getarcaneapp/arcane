// Package container owns Docker container lifecycle, inspection, listing and
// stats, plus the HTTP surface that exposes them.
package container

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Dependencies are the collaborators the container domain needs.
type Dependencies struct {
	Event    *event.EventService
	Docker   *docker.DockerClientService
	Image    *image.ImageService
	Settings *settings.SettingsService
	Project  *project.ProjectService
	Activity *activity.ActivityService
}

// Module wires the container domain and mounts its routes.
type Module struct {
	service *ContainerService
	deps    Dependencies
}

// New builds the container domain from its dependencies.
func New(ctx context.Context, deps Dependencies) *Module {
	return &Module{
		service: NewContainerService(ctx, deps.Event, deps.Docker, deps.Image, deps.Settings, deps.Project),
		deps:    deps,
	}
}

// Service exposes the container service to collaborators that use it directly.
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
	RegisterContainers(api, m.service, m.deps.Docker, m.deps.Settings, m.deps.Activity, appCtx)
}
