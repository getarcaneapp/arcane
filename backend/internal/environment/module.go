// Package environment owns environment persistence, remote runtime state, pairing,
// synchronization, and its HTTP and stream surfaces.
package environment

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Dependencies struct {
	Settings *settings.SettingsService
	ApiKey   *apikey.ApiKeyService
	Event    *event.EventService
	Config   *config.Config
}

type Module struct {
	service *EnvironmentService
	handler *EnvironmentHandler
}

func New(service *EnvironmentService, deps Dependencies) *Module {
	return &Module{
		service: service,
		handler: NewHandler(service, deps.Settings, deps.ApiKey, deps.Event, deps.Config),
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

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterEnvironments(api, NewHandler(nil, nil, nil, nil, nil))
		return
	}
	RegisterEnvironments(api, m.handler)
}
