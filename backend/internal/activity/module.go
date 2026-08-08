// Package activity owns background activity persistence, execution tracking,
// streaming, and the HTTP surface used to inspect and cancel work.
package activity

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Dependencies struct {
	DB          *database.DB
	Settings    *settings.SettingsService
	Environment EnvironmentDependencies
}

type Module struct {
	service *ActivityService
	handler *ActivityHandler
}

func New(deps Dependencies) *Module {
	service := NewActivityService(deps.DB, deps.Settings)
	return &Module{service: service, handler: NewHandler(service, deps.Environment)}
}

func (m *Module) Service() *ActivityService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) Handler() *ActivityHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterActivities(api, NewHandler(nil, EnvironmentDependencies{}))
		return
	}
	RegisterActivities(api, m.handler)
}
