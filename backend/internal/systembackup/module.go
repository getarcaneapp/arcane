// Package systembackup owns Arcane system-recovery backup orchestration and its HTTP surface.
package systembackup

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service  *SystemBackupService
	activity *activity.ActivityService
}

func New(service *SystemBackupService, activityService *activity.ActivityService) *Module {
	return &Module{service: service, activity: activityService}
}

func (m *Module) Service() *SystemBackupService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterSystemBackups(api, nil, nil, appCtx)
		return
	}
	RegisterSystemBackups(api, m.service, m.activity, appCtx)
}
