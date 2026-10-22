package group

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *GroupService
}

func New(service *GroupService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *GroupService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	RegisterGroups(api, m.Service())
}

func RegisterGroups(api huma.API, service *GroupService) {
	h := &GroupHandler{service: service}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-groups",
		Method:      http.MethodGet,
		Path:        "/groups",
		Summary:     "List groups",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsList, h.List)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-group",
		Method:      http.MethodPost,
		Path:        "/groups",
		Summary:     "Create a group",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsCreate, h.Create)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-group",
		Method:      http.MethodGet,
		Path:        "/groups/{id}",
		Summary:     "Get a group",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsRead, h.Get)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-group",
		Method:      http.MethodPatch,
		Path:        "/groups/{id}",
		Summary:     "Update a group",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsUpdate, h.Update)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-group",
		Method:      http.MethodDelete,
		Path:        "/groups/{id}",
		Summary:     "Delete a group",
		Description: "Removes the group with its memberships, bindings, and OIDC mappings. The caller must be allowed to delete every binding on the group.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsDelete, h.Delete)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-group-members",
		Method:      http.MethodGet,
		Path:        "/groups/{id}/members",
		Summary:     "List group members",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsRead, h.ListMembers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "add-group-members",
		Method:      http.MethodPost,
		Path:        "/groups/{id}/members",
		Summary:     "Add group members",
		Description: "The caller must hold every permission the group's bindings grant.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsMembers, h.AddMembers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "remove-group-member",
		Method:      http.MethodDelete,
		Path:        "/groups/{id}/members/{userId}",
		Summary:     "Remove a group member",
		Description: "The caller must hold every permission the group's bindings grant; the last global admin cannot be removed.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsMembers, h.RemoveMember)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-oidc-group-mappings",
		Method:      http.MethodGet,
		Path:        "/oidc/mappings",
		Summary:     "List OIDC claim to group mappings",
		Description: "On each OIDC login the user's group claim values are matched against these mappings and become source='oidc' group memberships.",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermOidcMappingsList, h.ListOidcMappings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-oidc-group-mapping",
		Method:      http.MethodPost,
		Path:        "/oidc/mappings",
		Summary:     "Create an OIDC group mapping",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermOidcMappingsCreate, h.CreateOidcMapping)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-oidc-group-mapping",
		Method:      http.MethodPatch,
		Path:        "/oidc/mappings/{id}",
		Summary:     "Update an OIDC group mapping",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermOidcMappingsUpdate, h.UpdateOidcMapping)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-oidc-group-mapping",
		Method:      http.MethodDelete,
		Path:        "/oidc/mappings/{id}",
		Summary:     "Delete an OIDC group mapping",
		Tags:        []string{"OIDC"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermOidcMappingsDelete, h.DeleteOidcMapping)
}
