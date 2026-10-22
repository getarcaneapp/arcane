// Package user owns user persistence, password hashing, authorization guards,
// and the user HTTP surface.
package user

import (
	"context"

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
	applyDefaultRole         func(context.Context, string) error
}

func New(service *UserService, invalidateUserTokenCache func(string), settingsService *settings.SettingsService, applyDefaultRole func(context.Context, string) error) *Module {
	return &Module{
		service:                  service,
		invalidateUserTokenCache: invalidateUserTokenCache,
		settings:                 settingsService,
		applyDefaultRole:         applyDefaultRole,
	}
}

func (m *Module) Service() *UserService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterUsers(api, nil, nil, nil, nil)
		return
	}
	RegisterUsers(api, m.service, m.invalidateUserTokenCache, m.settings, m.applyDefaultRole)
}

// RegisterUsers registers all user management endpoints.
func RegisterUsers(
	api huma.API,
	userService *UserService,
	invalidateUserTokenCache func(string),
	settingsService *settings.SettingsService,
	applyDefaultRole func(context.Context, string) error,
) {
	h := &UserHandler{
		userService:              userService,
		invalidateUserTokenCache: invalidateUserTokenCache,
		settingsService:          settingsService,
		applyDefaultRole:         applyDefaultRole,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listUsers",
		Method:      "GET",
		Path:        "/users",
		Summary:     "List users",
		Description: "Get a paginated list of all users",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermUsersList, h.ListUsers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createUser",
		Method:      "POST",
		Path:        "/users",
		Summary:     "Create a user",
		Description: "Create a new user account",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermUsersCreate, h.CreateUser)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getUser",
		Method:      "GET",
		Path:        "/users/{userId}",
		Summary:     "Get a user",
		Description: "Get a user by ID",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermUsersRead, h.GetUser)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateUser",
		Method:      "PATCH",
		Path:        "/users/{userId}",
		Summary:     "Update a user",
		Description: "Update an existing user's information",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermUsersUpdate, h.UpdateUser)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteUser",
		Method:      "DELETE",
		Path:        "/users/{userId}",
		Summary:     "Delete a user",
		Description: "Delete a user by ID",
		Tags:        []string{"Users"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermUsersDelete, h.DeleteUser)

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
