package binding

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-role-bindings",
		Method:      http.MethodGet,
		Path:        "/roles/bindings",
		Summary:     "List role bindings",
		Description: "List bindings of roles to users and groups, optionally filtered by subject, role, or environment",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBindingsList, h.List)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-role-binding",
		Method:      http.MethodGet,
		Path:        "/roles/bindings/{id}",
		Summary:     "Get a role binding",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBindingsList, h.Get)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-role-binding",
		Method:      http.MethodPost,
		Path:        "/roles/bindings",
		Summary:     "Create a role binding",
		Description: "Bind a role to a user or group, globally or in one environment. The caller must hold every granted permission at the requested scope.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBindingsCreate, h.Create)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-role-binding",
		Method:      http.MethodDelete,
		Path:        "/roles/bindings/{id}",
		Summary:     "Delete a role binding",
		Description: "The caller must hold every permission the binding grants at its scope; the last global Admin binding cannot be removed.",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBindingsDelete, h.Delete)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-user-effective-access",
		Method:      http.MethodGet,
		Path:        "/users/{userId}/effective-access",
		Summary:     "Get a user's effective access",
		Description: "Resolve the permissions a user holds per scope and the bindings that grant them",
		Tags:        []string{"Roles"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBindingsList, h.UserEffectiveAccess)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "get-my-effective-access",
		Method:      http.MethodGet,
		Path:        "/auth/me/effective-access",
		Summary:     "Get my effective access",
		Description: "Resolve the caller's permissions per scope and the bindings that grant them",
		Tags:        []string{"Auth"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.MyEffectiveAccess)
}
