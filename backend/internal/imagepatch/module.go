// Package imagepatch owns Copacetic-based image patching, persistence, and HTTP routes.
package imagepatch

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service *ImagePatchService
}

func New(service *ImagePatchService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *ImagePatchService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterImagePatches(api, nil, appCtx)
		return
	}
	RegisterImagePatches(api, m.service, appCtx)
}
