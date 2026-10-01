// Package image owns Docker image operations, metadata, attestations, and routes.
package image

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service     *ImageService
	docker      *docker.DockerClientService
	imageUpdate *imageupdate.ImageUpdateService
	settings    *settings.SettingsService
	build       *build.BuildService
	activity    *activity.ActivityService
	upload      *upload.UploadService
}

func New(service *ImageService, dockerService *docker.DockerClientService, imageUpdate *imageupdate.ImageUpdateService, settingsService *settings.SettingsService, buildService *build.BuildService, activityService *activity.ActivityService, uploadService *upload.UploadService) *Module {
	return &Module{service: service, docker: dockerService, imageUpdate: imageUpdate, settings: settingsService, build: buildService, activity: activityService, upload: uploadService}
}

func (m *Module) Service() *ImageService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterImages(api, nil, nil, nil, nil, nil, nil, nil, appCtx)
		return
	}
	RegisterImages(api, m.docker, m.service, m.imageUpdate, m.settings, m.build, m.activity, m.upload, appCtx)
}
