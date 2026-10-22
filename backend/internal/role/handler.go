package role

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type RoleHandler struct {
	roleService *RoleService
}

type ListRolesInput struct {
	Search string `query:"search" doc:"Search by role name or description"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type GetRoleInput struct {
	ID string `path:"id" doc:"Role ID"`
}

type CreateRoleInput struct {
	types.Create[types.Role]
}

type UpdateRoleInput struct {
	ID string `path:"id" doc:"Role ID"`
	types.Update[types.Role]
}

type DeleteRoleInput struct {
	ID string `path:"id" doc:"Role ID"`
}

func (h *RoleHandler) ListRoles(ctx context.Context, input *ListRolesInput) (*handlerutil.Page[types.Role], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	roles, paginationResp, err := h.roleService.ListRoles(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list roles: " + err.Error())
	}
	dtos := make([]types.Role, len(roles))
	for i := range roles {
		dtos[i] = h.toRoleDTO(ctx, &roles[i])
	}
	return &handlerutil.Page[types.Role]{
		Body: types.Paginated[types.Role]{
			Success:    true,
			Data:       dtos,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *RoleHandler) GetRole(ctx context.Context, input *GetRoleInput) (*handlerutil.Out[types.Role], error) {
	role, err := h.roleService.GetRole(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrRoleNotFound) {
			return nil, huma.Error404NotFound("role not found")
		}
		return nil, huma.Error500InternalServerError("failed to get role: " + err.Error())
	}
	return &handlerutil.Out[types.Role]{
		Body: types.ApiResponse[types.Role]{Success: true, Data: h.toRoleDTO(ctx, role)},
	}, nil
}

func (h *RoleHandler) CreateRole(ctx context.Context, input *CreateRoleInput) (*handlerutil.Out[types.Role], error) {
	callerPS, _ := authz.PermissionsFromContext(ctx)
	if err := h.roleService.ValidateRoleDefinitionAgainstCaller(callerPS, input.Body.Permissions); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	role, err := h.roleService.CreateRole(ctx, input.Body.Name, input.Body.Description, input.Body.Permissions)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &handlerutil.Out[types.Role]{
		Body: types.ApiResponse[types.Role]{Success: true, Data: h.toRoleDTO(ctx, role)},
	}, nil
}

func (h *RoleHandler) UpdateRole(ctx context.Context, input *UpdateRoleInput) (*handlerutil.Out[types.Role], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}
	current, err := h.roleService.GetRole(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	permissions := kit.Ternary(submitted["permissions"], input.Body.Permissions, []string(current.Permissions))
	callerPS, _ := authz.PermissionsFromContext(ctx)
	if validateErr := h.roleService.ValidateRoleDefinitionAgainstCaller(callerPS, permissions); validateErr != nil {
		apiErr := common.ToAPIError(validateErr)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	role, err := h.roleService.UpdateRole(
		ctx,
		callerPS,
		input.ID,
		kit.Ternary(submitted["name"], input.Body.Name, current.Name),
		kit.Ternary(submitted["description"], input.Body.Description, current.Description),
		permissions,
	)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &handlerutil.Out[types.Role]{
		Body: types.ApiResponse[types.Role]{Success: true, Data: h.toRoleDTO(ctx, role)},
	}, nil
}

func (h *RoleHandler) DeleteRole(ctx context.Context, input *DeleteRoleInput) (*handlerutil.Out[types.MessageResponse], error) {
	callerPS, _ := authz.PermissionsFromContext(ctx)
	if err := h.roleService.DeleteRole(ctx, callerPS, input.ID); err != nil {
		switch {
		case errors.Is(err, common.ErrRoleNotFound):
			return nil, huma.Error404NotFound("role not found")
		case errors.Is(err, common.ErrRoleBuiltIn):
			return nil, huma.Error403Forbidden("built-in roles cannot be deleted")
		case errors.Is(err, common.ErrRolePermissionEscalation):
			return nil, huma.Error403Forbidden(err.Error())
		case errors.Is(err, common.ErrRoleUsedByFederatedCredential):
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to delete role: " + err.Error())
	}
	return handlerutil.MessageOutput("role deleted", ""), nil
}

func (h *RoleHandler) GetPermissionsManifest(_ context.Context, _ *struct{}) (*handlerutil.Out[types.PermissionsManifest], error) {
	return &handlerutil.Out[types.PermissionsManifest]{
		Body: types.ApiResponse[types.PermissionsManifest]{Success: true, Data: BuildPermissionsManifest()},
	}, nil
}

func (h *RoleHandler) toRoleDTO(ctx context.Context, r *Role) types.Role {
	out := types.Role{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: []string(r.Permissions),
		BuiltIn:     r.BuiltIn,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if count, err := h.roleService.CountBindingsForRole(ctx, r.ID); err == nil {
		out.BindingCount = count
	}
	return out
}
