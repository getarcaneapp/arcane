// Package container owns Docker container lifecycle, inspection, listing and
// stats, plus the HTTP surface that exposes them.
package container

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Module wires the container domain and mounts its routes.
type Module struct {
	service         *ContainerService
	dockerService   *docker.DockerClientService
	settingsService *settings.SettingsService
	activityService *activity.ActivityService
}

// New wires container routes around an existing service.
func New(service *ContainerService, dockerService *docker.DockerClientService, settingsService *settings.SettingsService, activityService *activity.ActivityService) *Module {
	return &Module{service: service, dockerService: dockerService, settingsService: settingsService, activityService: activityService}
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
