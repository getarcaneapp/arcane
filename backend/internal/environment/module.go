// Package environment owns environment persistence, remote runtime state, pairing,
// synchronization, and its HTTP and stream surfaces.
package environment

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Module struct {
	service *EnvironmentService
	handler *EnvironmentHandler
}

func New(service *EnvironmentService, settingsService *settings.SettingsService, apiKey *apikey.ApiKeyService, eventService *event.EventService, cfg *config.Config, activityService activitylib.Service) *Module {
	return &Module{
		service: service,
		handler: NewHandler(service, settingsService, apiKey, eventService, cfg, activityService),
	}
}

func (m *Module) Service() *EnvironmentService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) Handler() *EnvironmentHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterEnvironments(api, NewHandler(nil, nil, nil, nil, nil, nil))
		return
	}
	m.handler.appCtx = appCtx.Context()
	RegisterEnvironments(api, m.handler)
}
