// Package imageupdate owns image update checks, persistence, and HTTP routes.
package imageupdate

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
)

type Dependencies struct {
	GetUpdateInfoByImageRefs func(context.Context, []string) (map[string]*imagetypes.UpdateInfo, error)
}

type Module struct {
	service *ImageUpdateService
	deps    Dependencies
}

func New(service *ImageUpdateService, deps Dependencies) *Module {
	return &Module{service: service, deps: deps}
}

func (m *Module) Service() *ImageUpdateService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterImageUpdates(api, nil, nil, appCtx)
		return
	}
	RegisterImageUpdates(api, m.service, m.deps.GetUpdateInfoByImageRefs, appCtx)
}
