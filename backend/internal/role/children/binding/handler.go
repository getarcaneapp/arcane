package binding

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Handler struct {
	service *BindingService
}

type ListBindingsInput struct {
	SubjectType   string `query:"subjectType" doc:"Filter by subject kind (user or group)"`
	SubjectID     string `query:"subjectId" doc:"Filter by user or group ID"`
	RoleID        string `query:"roleId" doc:"Filter by role ID"`
	EnvironmentID string `query:"environmentId" doc:"Filter by environment ID"`
	Search        string `query:"search" doc:"Search by subject, role, or environment name"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Items per page"`
}

type BindingIDInput struct {
	ID string `path:"id" doc:"Role binding ID"`
}

type CreateBindingInput struct {
	Body types.CreateRoleBinding
}

type UserEffectiveAccessInput struct {
	UserID string `path:"userId" doc:"User ID"`
}

func NewHandler(service *BindingService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(ctx context.Context, input *ListBindingsInput) (*handlerutil.Page[types.RoleBinding], error) {
	bindings, page, err := h.service.List(
		ctx,
		handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search),
		types.RoleBindingFilter{
			SubjectType:   input.SubjectType,
			SubjectID:     input.SubjectID,
			RoleID:        input.RoleID,
			EnvironmentID: input.EnvironmentID,
		},
	)
	if err != nil {
		return nil, bindingHTTPError(err)
	}
	return &handlerutil.Page[types.RoleBinding]{
		Body: types.Paginated[types.RoleBinding]{
			Success:    true,
			Data:       bindings,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *Handler) Get(ctx context.Context, input *BindingIDInput) (*handlerutil.Out[types.RoleBinding], error) {
	binding, err := h.service.Get(ctx, input.ID)
	if err != nil {
		return nil, bindingHTTPError(err)
	}
	return &handlerutil.Out[types.RoleBinding]{
		Body: types.ApiResponse[types.RoleBinding]{
			Success: true,
			Data:    binding,
		},
	}, nil
}

func (h *Handler) Create(ctx context.Context, input *CreateBindingInput) (*handlerutil.Out[types.CreateRoleBindingResult], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	actorID, _ := middleware.GetUserIDFromContext(ctx)
	binding, dropped, err := h.service.Create(ctx, caller, actorID, input.Body)
	if err != nil {
		return nil, bindingHTTPError(err)
	}
	return &handlerutil.Out[types.CreateRoleBindingResult]{
		Body: types.ApiResponse[types.CreateRoleBindingResult]{
			Success: true,
			Data: types.CreateRoleBindingResult{
				Binding:            binding,
				DroppedPermissions: append([]string{}, dropped...),
			},
		},
	}, nil
}

func (h *Handler) Delete(ctx context.Context, input *BindingIDInput) (*handlerutil.Out[types.MessageResponse], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	if err := h.service.Delete(ctx, caller, input.ID); err != nil {
		return nil, bindingHTTPError(err)
	}
	return handlerutil.MessageOutput("role binding deleted", ""), nil
}

func (h *Handler) UserEffectiveAccess(ctx context.Context, input *UserEffectiveAccessInput) (*handlerutil.Out[types.EffectiveAccess], error) {
	access, err := h.service.EffectiveAccess(ctx, input.UserID)
	if err != nil {
		return nil, bindingHTTPError(err)
	}
	return &handlerutil.Out[types.EffectiveAccess]{
		Body: types.ApiResponse[types.EffectiveAccess]{
			Success: true,
			Data:    access,
		},
	}, nil
}

func (h *Handler) MyEffectiveAccess(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.EffectiveAccess], error) {
	userID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	return h.UserEffectiveAccess(ctx, &UserEffectiveAccessInput{UserID: userID})
}

func bindingHTTPError(err error) error {
	apiErr := common.ToAPIError(err)
	return huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
}
