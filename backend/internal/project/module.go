// Package project owns compose project persistence, lifecycle, discovery, and routes.
package project

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service  *ProjectService
	activity *activity.ActivityService
}

func New(service *ProjectService, activityService *activity.ActivityService) *Module {
	return &Module{service: service, activity: activityService}
}

func (m *Module) Service() *ProjectService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterProjects(api, nil, nil, appCtx)
		return
	}
	RegisterProjects(api, m.service, m.activity, appCtx)
}
