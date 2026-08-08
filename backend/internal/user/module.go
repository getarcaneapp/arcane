// Package user owns user persistence, password hashing, authorization guards,
// and the user HTTP surface.
package user

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Dependencies struct {
	Service                  *UserService
	InvalidateUserTokenCache func(string)
	Settings                 *settings.SettingsService
}

type Module struct {
	deps Dependencies
}

func New(deps Dependencies) *Module {
	return &Module{deps: deps}
}

func (m *Module) Service() *UserService {
	if m == nil {
		return nil
	}
	return m.deps.Service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterUsers(api, nil, nil, nil)
		return
	}
	RegisterUsers(api, m.deps.Service, m.deps.InvalidateUserTokenCache, m.deps.Settings)
}
