package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/types/base"
	roletypes "github.com/getarcaneapp/arcane/types/role"
)

type RoleHandler struct {
	roleService *services.RoleService
}

// ---------- I/O wrappers ----------

type RolePaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []roletypes.Role        `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListRolesInput struct {
	Search string `query:"search" doc:"Search by role name or description"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListRolesOutput struct {
	Body RolePaginatedResponse
}

type GetRoleInput struct {
	ID string `path:"id" doc:"Role ID"`
}

type GetRoleOutput struct {
	Body base.ApiResponse[roletypes.Role]
}

type CreateRoleInput struct {
	Body roletypes.CreateRole
}

type CreateRoleOutput struct {
	Body base.ApiResponse[roletypes.Role]
}

type UpdateRoleInput struct {
	ID   string `path:"id" doc:"Role ID"`
	Body roletypes.UpdateRole
}

type UpdateRoleOutput struct {
	Body base.ApiResponse[roletypes.Role]
}

type DeleteRoleInput struct {
	ID string `path:"id" doc:"Role ID"`
}

type DeleteRoleOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PermissionsManifestOutput struct {
	Body base.ApiResponse[roletypes.PermissionsManifest]
}

type ListUserRoleAssignmentsInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

type ListUserRoleAssignmentsOutput struct {
	Body base.ApiResponse[[]roletypes.RoleAssignment]
}

type SetUserRoleAssignmentsInput struct {
	UserID string `path:"userId" doc:"User ID"`
	Body   roletypes.SetUserAssignments
}

type SetUserRoleAssignmentsOutput struct {
	Body base.ApiResponse[[]roletypes.RoleAssignment]
}

// ---------- Registration ----------

func RegisterRoles(api huma.API, roleService *services.RoleService) {
	h := &RoleHandler{roleService: roleService}

	huma.Register(api, huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/roles",
		Summary:     "List roles",
		Description: "Get a paginated list of roles (built-in + custom)",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermRolesList),
	}, h.ListRoles)

	huma.Register(api, huma.Operation{
		OperationID: "get-role",
		Method:      http.MethodGet,
		Path:        "/roles/{id}",
		Summary:     "Get a role",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermRolesRead),
	}, h.GetRole)

	huma.Register(api, huma.Operation{
		OperationID: "create-role",
		Method:      http.MethodPost,
		Path:        "/roles",
		Summary:     "Create a custom role",
		Description: "Built-in roles cannot be created via this endpoint; only custom roles are accepted. Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.CreateRole)

	huma.Register(api, huma.Operation{
		OperationID: "update-role",
		Method:      http.MethodPut,
		Path:        "/roles/{id}",
		Summary:     "Update a custom role",
		Description: "Built-in roles are read-only and return 403 on update. Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.UpdateRole)

	huma.Register(api, huma.Operation{
		OperationID: "delete-role",
		Method:      http.MethodDelete,
		Path:        "/roles/{id}",
		Summary:     "Delete a custom role",
		Description: "Built-in roles are protected; deleting cascades all user assignments. Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.DeleteRole)

	huma.Register(api, huma.Operation{
		OperationID: "get-permissions-manifest",
		Method:      http.MethodGet,
		Path:        "/roles/available-permissions",
		Summary:     "Get the permission manifest",
		Description: "Returns every permission the server recognizes, grouped by resource. Used by permission-picking UIs.",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetPermissionsManifest)

	huma.Register(api, huma.Operation{
		OperationID: "list-user-role-assignments",
		Method:      http.MethodGet,
		Path:        "/users/{userId}/role-assignments",
		Summary:     "List a user's role assignments",
		Description: "Reserved for global admins.",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.ListUserRoleAssignments)

	huma.Register(api, huma.Operation{
		OperationID: "set-user-role-assignments",
		Method:      http.MethodPut,
		Path:        "/users/{userId}/role-assignments",
		Summary:     "Replace a user's manual role assignments",
		Description: "Replaces every source='manual' assignment for the user. source='oidc' assignments are not touched. Reserved for global admins; enforces the last-admin guard.",
		Tags:        []string{"Roles"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.SetUserRoleAssignments)
}

// ---------- Handler implementations ----------

func (h *RoleHandler) ListRoles(ctx context.Context, input *ListRolesInput) (*ListRolesOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	roles, paginationResp, err := h.roleService.ListRoles(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list roles: " + err.Error())
	}
	dtos := make([]roletypes.Role, len(roles))
	for i := range roles {
		dtos[i] = h.toRoleDTO(ctx, &roles[i])
	}
	return &ListRolesOutput{
		Body: RolePaginatedResponse{
			Success: true,
			Data:    dtos,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

func (h *RoleHandler) GetRole(ctx context.Context, input *GetRoleInput) (*GetRoleOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	role, err := h.roleService.GetRole(ctx, input.ID)
	if err != nil {
		if common.IsRoleNotFoundError(err) {
			return nil, huma.Error404NotFound("role not found")
		}
		return nil, huma.Error500InternalServerError("failed to get role: " + err.Error())
	}
	return &GetRoleOutput{
		Body: base.ApiResponse[roletypes.Role]{Success: true, Data: h.toRoleDTO(ctx, role)},
	}, nil
}

func (h *RoleHandler) CreateRole(ctx context.Context, input *CreateRoleInput) (*CreateRoleOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	callerPS, _ := humamw.PermissionsFromContext(ctx)
	if err := h.roleService.ValidatePermissionsAgainstCaller(callerPS, input.Body.Permissions); err != nil {
		switch {
		case common.IsUnknownPermissionError(err):
			return nil, huma.Error400BadRequest(err.Error())
		case common.IsRolePermissionEscalationError(err):
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to validate role permissions: " + err.Error())
	}
	role, err := h.roleService.CreateRole(ctx, input.Body.Name, input.Body.Description, input.Body.Permissions)
	if err != nil {
		if common.IsRoleNameTakenError(err) {
			return nil, huma.Error409Conflict("role name already in use")
		}
		if common.IsUnknownPermissionError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to create role: " + err.Error())
	}
	return &CreateRoleOutput{
		Body: base.ApiResponse[roletypes.Role]{Success: true, Data: h.toRoleDTO(ctx, role)},
	}, nil
}

func (h *RoleHandler) UpdateRole(ctx context.Context, input *UpdateRoleInput) (*UpdateRoleOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	callerPS, _ := humamw.PermissionsFromContext(ctx)
	if err := h.roleService.ValidatePermissionsAgainstCaller(callerPS, input.Body.Permissions); err != nil {
		switch {
		case common.IsUnknownPermissionError(err):
			return nil, huma.Error400BadRequest(err.Error())
		case common.IsRolePermissionEscalationError(err):
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to validate role permissions: " + err.Error())
	}
	role, err := h.roleService.UpdateRole(ctx, input.ID, input.Body.Name, input.Body.Description, input.Body.Permissions)
	if err != nil {
		switch {
		case common.IsRoleNotFoundError(err):
			return nil, huma.Error404NotFound("role not found")
		case common.IsRoleBuiltInError(err):
			return nil, huma.Error403Forbidden("built-in roles cannot be modified")
		case common.IsRoleNameTakenError(err):
			return nil, huma.Error409Conflict("role name already in use")
		case common.IsUnknownPermissionError(err):
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to update role: " + err.Error())
	}
	return &UpdateRoleOutput{
		Body: base.ApiResponse[roletypes.Role]{Success: true, Data: h.toRoleDTO(ctx, role)},
	}, nil
}

func (h *RoleHandler) DeleteRole(ctx context.Context, input *DeleteRoleInput) (*DeleteRoleOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.roleService.DeleteRole(ctx, input.ID); err != nil {
		switch {
		case common.IsRoleNotFoundError(err):
			return nil, huma.Error404NotFound("role not found")
		case common.IsRoleBuiltInError(err):
			return nil, huma.Error403Forbidden("built-in roles cannot be deleted")
		case common.IsNoGlobalAdminRemainsError(err):
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to delete role: " + err.Error())
	}
	return &DeleteRoleOutput{
		Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "role deleted"}},
	}, nil
}

func (h *RoleHandler) GetPermissionsManifest(_ context.Context, _ *struct{}) (*PermissionsManifestOutput, error) {
	return &PermissionsManifestOutput{
		Body: base.ApiResponse[roletypes.PermissionsManifest]{Success: true, Data: buildPermissionsManifestInternal()},
	}, nil
}

func (h *RoleHandler) ListUserRoleAssignments(ctx context.Context, input *ListUserRoleAssignmentsInput) (*ListUserRoleAssignmentsOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	rows, err := h.roleService.ListUserAssignments(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list assignments: " + err.Error())
	}
	dtos := make([]roletypes.RoleAssignment, len(rows))
	for i := range rows {
		dtos[i] = toAssignmentDTOInternal(&rows[i])
	}
	return &ListUserRoleAssignmentsOutput{
		Body: base.ApiResponse[[]roletypes.RoleAssignment]{Success: true, Data: dtos},
	}, nil
}

func (h *RoleHandler) SetUserRoleAssignments(ctx context.Context, input *SetUserRoleAssignmentsInput) (*SetUserRoleAssignmentsOutput, error) {
	if h.roleService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	desired := make([]models.UserRoleAssignment, len(input.Body.Assignments))
	for i, a := range input.Body.Assignments {
		desired[i] = models.UserRoleAssignment{RoleID: a.RoleID, EnvironmentID: a.EnvironmentID}
	}
	if err := h.roleService.SetUserAssignments(ctx, input.UserID, desired); err != nil {
		switch {
		case common.IsInvalidRoleAssignmentError(err):
			return nil, huma.Error400BadRequest(err.Error())
		case common.IsNoGlobalAdminRemainsError(err):
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to set assignments: " + err.Error())
	}
	rows, err := h.roleService.ListUserAssignments(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read back assignments: " + err.Error())
	}
	dtos := make([]roletypes.RoleAssignment, len(rows))
	for i := range rows {
		dtos[i] = toAssignmentDTOInternal(&rows[i])
	}
	return &SetUserRoleAssignmentsOutput{
		Body: base.ApiResponse[[]roletypes.RoleAssignment]{Success: true, Data: dtos},
	}, nil
}

// ---------- DTO mappers ----------

func (h *RoleHandler) toRoleDTO(ctx context.Context, r *models.Role) roletypes.Role {
	out := roletypes.Role{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: []string(r.Permissions),
		BuiltIn:     r.BuiltIn,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if count, err := h.roleService.CountUsersAssignedToRole(ctx, r.ID); err == nil {
		out.AssignedUserCount = count
	}
	return out
}

func toAssignmentDTOInternal(r *models.UserRoleAssignment) roletypes.RoleAssignment {
	return roletypes.RoleAssignment{
		ID:            r.ID,
		UserID:        r.UserID,
		RoleID:        r.RoleID,
		EnvironmentID: r.EnvironmentID,
		Source:        r.Source,
		CreatedAt:     r.CreatedAt,
	}
}

// buildPermissionsManifestInternal maps the authz-owned permission catalog into
// the public API manifest shape used by the frontend role editor.
func buildPermissionsManifestInternal() roletypes.PermissionsManifest {
	catalog := authz.PermissionCatalog()
	resources := make([]roletypes.PermissionResource, len(catalog))
	for i, resource := range catalog {
		actions := make([]roletypes.PermissionAction, len(resource.Actions))
		for j, action := range resource.Actions {
			actions[j] = roletypes.PermissionAction{
				Key:         action.Key,
				Permission:  action.Permission,
				Label:       action.Label,
				Description: action.Description,
				Requires:    requiredPermissionsForPickerInternal(action.Permission),
			}
		}
		resources[i] = roletypes.PermissionResource{
			Key:     resource.Key,
			Label:   resource.Label,
			Scope:   resource.Scope,
			Actions: actions,
		}
	}
	return roletypes.PermissionsManifest{
		Resources: resources,
		Presets: []roletypes.PermissionPreset{
			{
				Key:         "editor",
				Label:       "All permissions (non-admin)",
				Description: "Matches the built-in Editor permission set.",
				Permissions: authz.BuiltInEditorPermissions(),
			},
			{
				Key:         "global-admin",
				Label:       "Global Admin",
				Description: "Select every permission in the manifest.",
				Permissions: authz.AllPermissions(),
			},
		},
	}
}

func requiredPermissionsForPickerInternal(permission string) []string {
	switch permission {
	case authz.PermSettingsWrite,
		authz.PermApiKeysList,
		authz.PermApiKeysRead,
		authz.PermApiKeysCreate,
		authz.PermApiKeysUpdate,
		authz.PermApiKeysDelete,
		authz.PermFederatedList,
		authz.PermFederatedRead,
		authz.PermFederatedCreate,
		authz.PermFederatedUpdate,
		authz.PermFederatedDelete,
		authz.PermUsersList,
		authz.PermUsersRead,
		authz.PermUsersCreate,
		authz.PermUsersUpdate,
		authz.PermUsersDelete,
		authz.PermRolesList,
		authz.PermRolesRead,
		authz.PermWebhooksList,
		authz.PermWebhooksCreate,
		authz.PermWebhooksUpdate,
		authz.PermWebhooksDelete,
		authz.PermNotificationsManage,
		authz.PermDiagnosticsRead:
		return []string{authz.PermSettingsRead}
	default:
		return nil
	}
}
