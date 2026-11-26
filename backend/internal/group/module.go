package group

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service *GroupService
	}
)

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
	if m == nil {
		RegisterGroups(api, nil)
		return
	}
	RegisterGroups(api, m.service)
}

func RegisterGroups(api huma.API, groupService *GroupService) {
	h := &GroupHandler{groupService: groupService}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-groups",
		Method:      http.MethodGet,
		Path:        "/groups",
		Summary:     "List groups",
		Description: "Get a paginated list of user groups",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsList, h.ListGroups)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-group",
		Method:      http.MethodGet,
		Path:        "/groups/{id}",
		Summary:     "Get a group",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsRead, h.GetGroup)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "create-group",
		Method:      http.MethodPost,
		Path:        "/groups",
		Summary:     "Create a group",
		Description: "Create a user group. Reserved for global admins.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.CreateGroup)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "update-group",
		Method:      http.MethodPatch,
		Path:        "/groups/{id}",
		Summary:     "Update a group",
		Description: "Update a user group's name or description. Reserved for global admins.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.UpdateGroup)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "delete-group",
		Method:      http.MethodDelete,
		Path:        "/groups/{id}",
		Summary:     "Delete a group",
		Description: "Delete a user group and cascade its memberships and role assignments. Reserved for global admins; enforces the last-admin guard.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.DeleteGroup)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-group-members",
		Method:      http.MethodGet,
		Path:        "/groups/{id}/members",
		Summary:     "List group members",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsRead, h.ListGroupMembers)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "set-group-members",
		Method:      http.MethodPut,
		Path:        "/groups/{id}/members",
		Summary:     "Replace a group's manual members",
		Description: "Replaces every source='manual' membership for the group. IdP-synced memberships are not touched. Reserved for global admins; enforces the last-admin guard.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.SetGroupMembers)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-group-role-assignments",
		Method:      http.MethodGet,
		Path:        "/groups/{id}/roles",
		Summary:     "List a group's role assignments",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermGroupsRead, h.ListGroupRoleAssignments)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "set-group-role-assignments",
		Method:      http.MethodPut,
		Path:        "/groups/{id}/roles",
		Summary:     "Replace a group's manual role assignments",
		Description: "Replaces every source='manual' role assignment for the group. Reserved for global admins; enforces the last-admin guard.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.SetGroupRoleAssignments)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "list-group-idp-mappings",
		Method:      http.MethodGet,
		Path:        "/groups/idp-mappings",
		Summary:     "List IdP claim-to-group mappings",
		Description: "Reserved for global admins.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.ListGroupIdpMappings)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "create-group-idp-mapping",
		Method:      http.MethodPost,
		Path:        "/groups/idp-mappings",
		Summary:     "Create an IdP claim-to-group mapping",
		Description: "Map an OIDC or LDAP group claim value to an Arcane group. Reserved for global admins.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.CreateGroupIdpMapping)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "delete-group-idp-mapping",
		Method:      http.MethodDelete,
		Path:        "/groups/idp-mappings/{id}",
		Summary:     "Delete an IdP claim-to-group mapping",
		Description: "Reserved for global admins.",
		Tags:        []string{"Groups"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.DeleteGroupIdpMapping)
}
