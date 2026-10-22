// Package user owns user persistence, password hashing, authorization guards,
// and the user HTTP surface.
package user

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
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

// RegisterUsers registers all user management endpoints.
func RegisterUsers(api huma.API, userService *UserService, invalidateUserTokenCache func(string), settingsService *settings.SettingsService) {
	h := &UserHandler{userService: userService, invalidateUserTokenCache: invalidateUserTokenCache, settingsService: settingsService}

	huma.Register(api, huma.Operation{
		OperationID: "listUsers",
		Method:      "GET",
		Path:        "/users",
		Summary:     "List users",
		Description: "Get a paginated list of all users",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersList),
	}, h.ListUsers)

	huma.Register(api, huma.Operation{
		OperationID: "createUser",
		Method:      "POST",
		Path:        "/users",
		Summary:     "Create a user",
		Description: "Create a new user account",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersCreate),
	}, h.CreateUser)

	huma.Register(api, huma.Operation{
		OperationID: "getUser",
		Method:      "GET",
		Path:        "/users/{userId}",
		Summary:     "Get a user",
		Description: "Get a user by ID",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersRead),
	}, h.GetUser)

	huma.Register(api, huma.Operation{
		OperationID: "updateUser",
		Method:      "PATCH",
		Path:        "/users/{userId}",
		Summary:     "Update a user",
		Description: "Update an existing user's information",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersUpdate),
	}, h.UpdateUser)

	huma.Register(api, huma.Operation{
		OperationID: "deleteUser",
		Method:      "DELETE",
		Path:        "/users/{userId}",
		Summary:     "Delete a user",
		Description: "Delete a user by ID",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermUsersDelete),
	}, h.DeleteUser)

	// Unauthenticated by design: profile pictures are publicly visible
	// so they can be displayed without requiring a session token.
	huma.Register(api, huma.Operation{
		OperationID: "getUserAvatar",
		Method:      "GET",
		Path:        "/users/{userId}/avatar",
		Summary:     "Get user avatar",
		Description: "Get the custom profile picture for a user",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{},
	}, h.GetUserAvatar)
}
