// Package auth owns authentication, token issuance and verification, login
// session policy, and the authentication HTTP surface.
package auth

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service                *AuthService
	user                   *user.UserService
	settings               *settings.SettingsService
	beginMFAAuthentication func(context.Context, string, types.SessionMeta, string) (*types.MFAChallenge, error)
}

func New(
	service *AuthService,
	userService *user.UserService,
	settingsService *settings.SettingsService,
	beginMFAAuthentication func(
		context.Context,
		string,
		types.SessionMeta,
		string,
	) (
		*types.MFAChallenge,
		error,
	),
) *Module {
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

// RegisterAuth registers authentication routes using Huma.
func RegisterAuth(
	api huma.API,
	userService *user.UserService,
	authService *AuthService,
	settingsService *settings.SettingsService,
	beginMFAAuthentication func(
		context.Context,
		string,
		types.SessionMeta,
		string,
	) (
		*types.MFAChallenge,
		error,
	),
) {
	h := &AuthHandler{
		userService:            userService,
		authService:            authService,
		beginMFAAuthentication: beginMFAAuthentication,
		settingsService:        settingsService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/auth/login",
		Summary:     "Login",
		Description: "Authenticate a user with username and password",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: "logout",
		Method:      http.MethodPost,
		Path:        "/auth/logout",
		Summary:     "Logout",
		Description: "Clear authentication session",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{},
	}, h.Logout)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "get-current-user",
		Method:      http.MethodGet,
		Path:        "/auth/me",
		Summary:     "Get current user",
		Description: "Get the currently authenticated user's information",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetCurrentUser)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "get-session-bootstrap",
		Method:      http.MethodGet,
		Path:        "/auth/bootstrap",
		Summary:     "Get session bootstrap",
		Description: "Get the current user, app version, accessible environments, and permission manifest in one response",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetSessionBootstrap)

	huma.Register(api, huma.Operation{
		OperationID: "refresh-token",
		Method:      http.MethodPost,
		Path:        "/auth/refresh",
		Summary:     "Refresh token",
		Description: "Obtain a new access token using a refresh token",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{},
	}, h.RefreshToken)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "change-password",
		Method:      http.MethodPost,
		Path:        "/auth/password",
		Summary:     "Change password",
		Description: "Change the current user's password",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.ChangePassword)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "logout-all-other-sessions",
		Method:      http.MethodDelete,
		Path:        "/auth/sessions",
		Summary:     "Logout all other sessions",
		Description: "Revoke every session for the current user except the one making this request",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.LogoutAllOtherSessions)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "update-my-profile",
		Method:      http.MethodPatch,
		Path:        "/auth/me/profile",
		Summary:     "Update own profile",
		Description: "Update the current user's display name and email. Forbidden for OIDC-managed accounts.",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.UpdateMyProfile)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "upload-my-avatar",
		Method:      http.MethodPost,
		Path:        "/auth/me/avatar",
		Summary:     "Upload own avatar",
		Description: "Upload a custom profile picture (PNG, JPEG or WebP). Replaces any existing avatar.",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"file": {Type: "string", Format: "binary"},
						},
					},
				},
			},
		},
	}, h.UploadMyAvatar)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "delete-my-avatar",
		Method:      http.MethodDelete,
		Path:        "/auth/me/avatar",
		Summary:     "Delete own avatar",
		Description: "Remove the current user's custom profile picture, reverting to the default avatar.",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.DeleteMyAvatar)
}
