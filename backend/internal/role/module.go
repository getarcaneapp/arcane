// Package role owns RBAC roles, assignments, permission resolution, and role HTTP routes.
package role

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Module owns role persistence and its HTTP surface.
type Module struct {
	service *RoleService
}

// New assembles the role routes around its service.
func New(service *RoleService) *Module {
	return &Module{service: service}
}

// Service exposes role operations to authentication and authorization collaborators.
func (m *Module) Service() *RoleService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts role endpoints for runtime and schema discovery.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterRoles(api, nil)
		return
	}
	RegisterRoles(api, m.service)
}

func RegisterRoles(api huma.API, roleService *RoleService) {
	h := &RoleHandler{roleService: roleService}

	huma.Register(api, huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/roles",
		Summary:     "List roles",
		Description: "Get a paginated list of roles (built-in + custom)",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRolesList),
	}, h.ListRoles)

	huma.Register(api, huma.Operation{
		OperationID: "get-role",
		Method:      http.MethodGet,
		Path:        "/roles/{id}",
		Summary:     "Get a role",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRolesRead),
	}, h.GetRole)

	huma.Register(api, huma.Operation{
		OperationID: "create-role",
		Method:      http.MethodPost,
		Path:        "/roles",
		Summary:     "Create a custom role",
		Description: "Built-in roles cannot be created via this endpoint; only custom roles are accepted. Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.CreateRole)

	huma.Register(api, huma.Operation{
		OperationID: "update-role",
		Method:      http.MethodPatch,
		Path:        "/roles/{id}",
		Summary:     "Update a custom role",
		Description: "Built-in roles are read-only and return 403 on update. Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.UpdateRole)

	huma.Register(api, huma.Operation{
		OperationID: "delete-role",
		Method:      http.MethodDelete,
		Path:        "/roles/{id}",
		Summary:     "Delete a custom role",
		Description: "Built-in roles are protected; deleting cascades all user assignments. Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.DeleteRole)

	huma.Register(api, huma.Operation{
		OperationID: "get-permissions-manifest",
		Method:      http.MethodGet,
		Path:        "/roles/permissions",
		Summary:     "Get the permission manifest",
		Description: "Returns every permission the server recognizes, grouped by resource. Used by permission-picking UIs.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetPermissionsManifest)

	huma.Register(api, huma.Operation{
		OperationID: "list-user-role-assignments",
		Method:      http.MethodGet,
		Path:        "/users/{userId}/roles",
		Summary:     "List a user's role assignments",
		Description: "Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.ListUserRoleAssignments)

	huma.Register(api, huma.Operation{
		OperationID: "set-user-role-assignments",
		Method:      http.MethodPut,
		Path:        "/users/{userId}/roles",
		Summary:     "Replace a user's manual role assignments",
		Description: "Replaces every source='manual' assignment for the user. source='oidc' assignments are not touched. Reserved for global admins; enforces the last-admin guard.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.SetUserRoleAssignments)
}
