// Package auth owns authentication, token issuance and verification, login
// session policy, and the authentication HTTP surface.
package auth

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	authtypes "github.com/getarcaneapp/arcane/types/v2/auth"
)

type Dependencies struct {
	Service                *AuthService
	User                   *user.UserService
	Settings               *settings.SettingsService
	BeginMFAAuthentication func(context.Context, string, authtypes.SessionMeta, string) (*authtypes.MFAChallenge, error)
}

type Module struct {
	deps Dependencies
}

func New(deps Dependencies) *Module {
	return &Module{deps: deps}
}

func (m *Module) Service() *AuthService {
	if m == nil {
		return nil
	}
	return m.deps.Service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterAuth(api, nil, nil, nil, nil)
		return
	}
	RegisterAuth(api, m.deps.User, m.deps.Service, m.deps.Settings, m.deps.BeginMFAAuthentication)
}
