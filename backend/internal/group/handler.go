package group

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type GroupHandler struct {
	service *GroupService
}

type ListGroupsInput struct {
	Search string `query:"search" doc:"Search by group name or description"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type GroupIDInput struct {
	ID string `path:"id" doc:"Group ID"`
}

type CreateGroupInput struct {
	Body types.CreateGroup
}

type UpdateGroupInput struct {
	ID string `path:"id" doc:"Group ID"`
	types.Update[types.UpdateGroup]
}

type AddGroupMembersInput struct {
	ID   string `path:"id" doc:"Group ID"`
	Body types.AddGroupMembers
}

type RemoveGroupMemberInput struct {
	ID     string `path:"id" doc:"Group ID"`
	UserID string `path:"userId" doc:"User ID"`
}

type CreateOidcMappingInput struct {
	Body types.OidcGroupMappingSpec
}

type UpdateOidcMappingInput struct {
	ID   string `path:"id" doc:"Mapping ID"`
	Body types.OidcGroupMappingSpec
}

type OidcMappingIDInput struct {
	ID string `path:"id" doc:"Mapping ID"`
}

func groupHTTPError(err error) error {
	apiErr := common.ToAPIError(err)
	return huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
}

func (h *GroupHandler) List(ctx context.Context, input *ListGroupsInput) (*handlerutil.Page[types.Group], error) {
	groups, page, err := h.service.List(ctx, handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search))
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Page[types.Group]{
		Body: types.Paginated[types.Group]{
			Success:    true,
			Data:       groups,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *GroupHandler) Get(ctx context.Context, input *GroupIDInput) (*handlerutil.Out[types.Group], error) {
	group, err := h.service.Get(ctx, input.ID)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[types.Group]{
		Body: types.ApiResponse[types.Group]{
			Success: true,
			Data:    group,
		},
	}, nil
}

func (h *GroupHandler) Create(ctx context.Context, input *CreateGroupInput) (*handlerutil.Out[types.Group], error) {
	group, err := h.service.Create(ctx, input.Body)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[types.Group]{
		Body: types.ApiResponse[types.Group]{
			Success: true,
			Data:    group,
		},
	}, nil
}

func (h *GroupHandler) Update(ctx context.Context, input *UpdateGroupInput) (*handlerutil.Out[types.Group], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}
	group, err := h.service.Update(ctx, input.ID, input.Body, submitted)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[types.Group]{
		Body: types.ApiResponse[types.Group]{
			Success: true,
			Data:    group,
		},
	}, nil
}

func (h *GroupHandler) Delete(ctx context.Context, input *GroupIDInput) (*handlerutil.Out[types.MessageResponse], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	if err := h.service.Delete(ctx, caller, input.ID); err != nil {
		return nil, groupHTTPError(err)
	}
	return handlerutil.MessageOutput("group deleted", ""), nil
}

func (h *GroupHandler) ListMembers(ctx context.Context, input *GroupIDInput) (*handlerutil.Out[[]types.GroupMember], error) {
	members, err := h.service.ListMembers(ctx, input.ID)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[[]types.GroupMember]{
		Body: types.ApiResponse[[]types.GroupMember]{
			Success: true,
			Data:    members,
		},
	}, nil
}

func (h *GroupHandler) AddMembers(ctx context.Context, input *AddGroupMembersInput) (*handlerutil.Out[[]types.GroupMember], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	if err := h.service.AddMembers(ctx, caller, input.ID, input.Body.UserIDs); err != nil {
		return nil, groupHTTPError(err)
	}
	return h.ListMembers(ctx, &GroupIDInput{ID: input.ID})
}

func (h *GroupHandler) RemoveMember(ctx context.Context, input *RemoveGroupMemberInput) (*handlerutil.Out[types.MessageResponse], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	if err := h.service.RemoveMember(ctx, caller, input.ID, input.UserID); err != nil {
		return nil, groupHTTPError(err)
	}
	return handlerutil.MessageOutput("group member removed", ""), nil
}

func (h *GroupHandler) ListOidcMappings(ctx context.Context, _ *struct{}) (*handlerutil.Out[[]types.OidcGroupMapping], error) {
	mappings, err := h.service.ListOidcMappings(ctx)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[[]types.OidcGroupMapping]{
		Body: types.ApiResponse[[]types.OidcGroupMapping]{
			Success: true,
			Data:    mappings,
		},
	}, nil
}

func (h *GroupHandler) CreateOidcMapping(ctx context.Context, input *CreateOidcMappingInput) (*handlerutil.Out[types.OidcGroupMapping], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	mapping, err := h.service.CreateOidcMapping(ctx, caller, input.Body)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[types.OidcGroupMapping]{
		Body: types.ApiResponse[types.OidcGroupMapping]{
			Success: true,
			Data:    mapping,
		},
	}, nil
}

func (h *GroupHandler) UpdateOidcMapping(ctx context.Context, input *UpdateOidcMappingInput) (*handlerutil.Out[types.OidcGroupMapping], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	mapping, err := h.service.UpdateOidcMapping(ctx, caller, input.ID, input.Body)
	if err != nil {
		return nil, groupHTTPError(err)
	}
	return &handlerutil.Out[types.OidcGroupMapping]{
		Body: types.ApiResponse[types.OidcGroupMapping]{
			Success: true,
			Data:    mapping,
		},
	}, nil
}

func (h *GroupHandler) DeleteOidcMapping(ctx context.Context, input *OidcMappingIDInput) (*handlerutil.Out[types.MessageResponse], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	if err := h.service.DeleteOidcMapping(ctx, caller, input.ID); err != nil {
		return nil, groupHTTPError(err)
	}
	return handlerutil.MessageOutput("OIDC group mapping deleted", ""), nil
}
