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

type Dependencies struct {
	Docker      *docker.DockerClientService
	ImageUpdate *imageupdate.ImageUpdateService
	Settings    *settings.SettingsService
	Build       *build.BuildService
	Activity    *activity.ActivityService
	Upload      *upload.UploadService
}

type Module struct {
	service *ImageService
	deps    Dependencies
}

func New(service *ImageService, deps Dependencies) *Module {
	return &Module{service: service, deps: deps}
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
	RegisterImages(api, m.deps.Docker, m.service, m.deps.ImageUpdate, m.deps.Settings, m.deps.Build, m.deps.Activity, m.deps.Upload, appCtx)
}
