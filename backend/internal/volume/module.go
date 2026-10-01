// Package volume owns Docker volumes: CRUD and pruning, the helper-container
// file browser, backup and restore, and the HTTP surface for all of it.
package volume

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Module wires the volume domain and mounts its routes.
type Module struct {
	service            *VolumeService
	dockerService      *docker.DockerClientService
	activityService    *activity.ActivityService
	environmentService *environment.EnvironmentService
	uploadService      *upload.UploadService
}

// New wires volume routes around an existing service.
func New(service *VolumeService, dockerService *docker.DockerClientService, activityService *activity.ActivityService, environmentService *environment.EnvironmentService, uploadService *upload.UploadService) *Module {
	return &Module{service: service, dockerService: dockerService, activityService: activityService, environmentService: environmentService, uploadService: uploadService}
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
	RegisterVolumes(api, m.dockerService, m.service, m.activityService, m.environmentService, m.uploadService, appCtx)
}
