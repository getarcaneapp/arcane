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

type Module struct {
	service                *AuthService
	user                   *user.UserService
	settings               *settings.SettingsService
	beginMFAAuthentication func(context.Context, string, authtypes.SessionMeta, string) (*authtypes.MFAChallenge, error)
}

func New(service *AuthService, userService *user.UserService, settingsService *settings.SettingsService, beginMFAAuthentication func(context.Context, string, authtypes.SessionMeta, string) (*authtypes.MFAChallenge, error)) *Module {
	return &Module{service: service, user: userService, settings: settingsService, beginMFAAuthentication: beginMFAAuthentication}
}

func (m *Module) Service() *AuthService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterAuth(api, nil, nil, nil, nil)
		return
	}
	RegisterAuth(api, m.user, m.service, m.settings, m.beginMFAAuthentication)
}
