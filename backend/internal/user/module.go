// Package user owns user persistence, password hashing, authorization guards,
// and the user HTTP surface.
package user

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Module struct {
	service                  *UserService
	invalidateUserTokenCache func(string)
	settings                 *settings.SettingsService
}

func New(service *UserService, invalidateUserTokenCache func(string), settingsService *settings.SettingsService) *Module {
	return &Module{service: service, invalidateUserTokenCache: invalidateUserTokenCache, settings: settingsService}
}

func (m *Module) Service() *UserService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterUsers(api, nil, nil, nil)
		return
	}
	RegisterUsers(api, m.service, m.invalidateUserTokenCache, m.settings)
}
