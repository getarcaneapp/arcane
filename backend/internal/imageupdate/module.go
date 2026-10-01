// Package imageupdate owns image update checks, persistence, and HTTP routes.
package imageupdate

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
)

type Module struct {
	service                  *ImageUpdateService
	getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*imagetypes.UpdateInfo, error)
}

func New(service *ImageUpdateService, getUpdateInfo func(context.Context, []string) (map[string]*imagetypes.UpdateInfo, error)) *Module {
	return &Module{service: service, getUpdateInfoByImageRefs: getUpdateInfo}
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
	RegisterImageUpdates(api, m.service, m.getUpdateInfoByImageRefs, appCtx)
}
