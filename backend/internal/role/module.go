// Package role owns RBAC roles, assignments, permission resolution, and role HTTP routes.
package role

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role/children/binding"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Module owns role persistence and its HTTP surface.
type Module struct {
	service        *RoleService
	bindingHandler *binding.Handler
}

// New assembles the role routes around its service.
func New(service *RoleService) *Module {
	return &Module{
		service:        service,
		bindingHandler: binding.NewHandler(service.bindings),
	}
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
		binding.RegisterRoutes(api, binding.NewHandler(nil))
		return
	}
	RegisterRoles(api, m.service)
	binding.RegisterRoutes(api, m.bindingHandler)
}

func RegisterRoles(api huma.API, roleService *RoleService) {
	h := &RoleHandler{roleService: roleService}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/roles",
		Summary:     "List roles",
		Description: "Get a paginated list of roles (built-in + custom)",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRolesList, h.ListRoles)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-role",
		Method:      http.MethodGet,
		Path:        "/roles/{id}",
		Summary:     "Get a role",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRolesRead, h.GetRole)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-role",
		Method:      http.MethodPost,
		Path:        "/roles",
		Summary:     "Create a custom role",
		Description: "Built-in roles are defined in code. The caller must hold every permission of the new role unrestricted at global scope.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRolesCreate, h.CreateRole)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-role",
		Method:      http.MethodPatch,
		Path:        "/roles/{id}",
		Summary:     "Update a custom role",
		Description: "Built-in roles are read-only and return 403 on update. The caller must hold every permission of the updated role unrestricted at global scope.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRolesUpdate, h.UpdateRole)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-role",
		Method:      http.MethodDelete,
		Path:        "/roles/{id}",
		Summary:     "Delete a custom role",
		Description: "Built-in roles are protected; deleting removes every binding of the role, so the caller must be allowed to delete each of them.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermRolesDelete, h.DeleteRole)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "get-permissions-manifest",
		Method:      http.MethodGet,
		Path:        "/roles/permissions",
		Summary:     "Get the permission manifest",
		Description: "Returns every permission the server recognizes, grouped by resource. Used by permission-picking UIs.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetPermissionsManifest)
}
