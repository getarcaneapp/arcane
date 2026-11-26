package group

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	GroupHandler struct {
		groupService *GroupService
	}

	ListGroupsInput struct {
		Search string `query:"search" doc:"Search by group name or description"`
		Sort   string `query:"sort" doc:"Column to sort by"`
		Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
		Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
		Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	}

	GetGroupInput struct {
		ID string `path:"id" doc:"Group ID"`
	}

	CreateGroupInput struct {
		types.Create[types.Group]
	}

	UpdateGroupInput struct {
		ID string `path:"id" doc:"Group ID"`
		types.Update[types.Group]
	}

	DeleteGroupInput struct {
		ID string `path:"id" doc:"Group ID"`
	}

	ListGroupMembersInput struct {
		ID string `path:"id" doc:"Group ID"`
	}

	SetGroupMembersInput struct {
		ID   string `path:"id" doc:"Group ID"`
		Body types.SetGroupMembers
	}

	ListGroupRoleAssignmentsInput struct {
		ID string `path:"id" doc:"Group ID"`
	}

	SetGroupRoleAssignmentsInput struct {
		ID   string `path:"id" doc:"Group ID"`
		Body types.SetGroupRoleAssignments
	}
)

func (h *GroupHandler) ListGroups(ctx context.Context, input *ListGroupsInput) (*handlerutil.Page[types.Group], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	groups, paginationResp, err := h.groupService.ListGroups(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list groups: " + err.Error())
	}
	dtos := make([]types.Group, len(groups))
	for i := range groups {
		dto, dtoErr := h.toGroupDTO(ctx, &groups[i])
		if dtoErr != nil {
			return nil, huma.Error500InternalServerError("failed to load group counts: " + dtoErr.Error())
		}
		dtos[i] = dto
	}
	return &handlerutil.Page[types.Group]{
		Body: types.Paginated[types.Group]{
			Success:    true,
			Data:       dtos,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *GroupHandler) GetGroup(ctx context.Context, input *GetGroupInput) (*handlerutil.Out[types.Group], error) {
	group, err := h.groupService.GetGroup(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrGroupNotFound) {
			return nil, huma.Error404NotFound("group not found")
		}
		return nil, huma.Error500InternalServerError("failed to get group: " + err.Error())
	}
	dto, err := h.toGroupDTO(ctx, group)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load group counts: " + err.Error())
	}
	return &handlerutil.Out[types.Group]{
		Body: types.ApiResponse[types.Group]{Success: true, Data: dto},
	}, nil
}

func (h *GroupHandler) CreateGroup(ctx context.Context, input *CreateGroupInput) (*handlerutil.Out[types.Group], error) {
	var memberUserIDs *[]string
	if len(input.Body.MemberUserIDs) > 0 {
		ids := input.Body.MemberUserIDs
		memberUserIDs = &ids
	}
	var roleAssignments *[]GroupRoleAssignment
	if len(input.Body.RoleAssignments) > 0 {
		desired := toGroupRoleAssignmentModels(input.Body.RoleAssignments)
		roleAssignments = &desired
	}
	group, err := h.groupService.CreateGroup(ctx, GroupMutation{
		Name:            input.Body.Name,
		Description:     input.Body.Description,
		MemberUserIDs:   memberUserIDs,
		RoleAssignments: roleAssignments,
	})
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	dto, err := h.toGroupDTO(ctx, group)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load group counts: " + err.Error())
	}
	return &handlerutil.Out[types.Group]{
		Body: types.ApiResponse[types.Group]{Success: true, Data: dto},
	}, nil
}

func (h *GroupHandler) UpdateGroup(ctx context.Context, input *UpdateGroupInput) (*handlerutil.Out[types.Group], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}
	current, err := h.groupService.GetGroup(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	var memberUserIDs *[]string
	if submitted["memberUserIds"] {
		ids := input.Body.MemberUserIDs
		memberUserIDs = &ids
	}
	var roleAssignments *[]GroupRoleAssignment
	if submitted["roleAssignments"] {
		desired := toGroupRoleAssignmentModels(input.Body.RoleAssignments)
		roleAssignments = &desired
	}
	group, err := h.groupService.UpdateGroup(ctx, input.ID, GroupMutation{
		Name:            kit.Ternary(submitted["name"], input.Body.Name, current.Name),
		Description:     kit.Ternary(submitted["description"], input.Body.Description, current.Description),
		MemberUserIDs:   memberUserIDs,
		RoleAssignments: roleAssignments,
	})
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	dto, err := h.toGroupDTO(ctx, group)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to load group counts: " + err.Error())
	}
	return &handlerutil.Out[types.Group]{
		Body: types.ApiResponse[types.Group]{Success: true, Data: dto},
	}, nil
}

func (h *GroupHandler) DeleteGroup(ctx context.Context, input *DeleteGroupInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.groupService.DeleteGroup(ctx, input.ID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return handlerutil.MessageOutput("group deleted", ""), nil
}

func (h *GroupHandler) ListGroupMembers(ctx context.Context, input *ListGroupMembersInput) (*handlerutil.Out[[]types.GroupMember], error) {
	rows, err := h.groupService.ListMembers(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	dtos := make([]types.GroupMember, len(rows))
	for i := range rows {
		dtos[i] = toGroupMemberDTO(&rows[i])
	}
	return &handlerutil.Out[[]types.GroupMember]{
		Body: types.ApiResponse[[]types.GroupMember]{Success: true, Data: dtos},
	}, nil
}

func (h *GroupHandler) SetGroupMembers(ctx context.Context, input *SetGroupMembersInput) (*handlerutil.Out[[]types.GroupMember], error) {
	if err := h.groupService.ReplaceMembers(ctx, input.ID, SourceManual, input.Body.UserIDs); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	rows, err := h.groupService.ListMembers(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read back group members: " + err.Error())
	}
	dtos := make([]types.GroupMember, len(rows))
	for i := range rows {
		dtos[i] = toGroupMemberDTO(&rows[i])
	}
	return &handlerutil.Out[[]types.GroupMember]{
		Body: types.ApiResponse[[]types.GroupMember]{Success: true, Data: dtos},
	}, nil
}

func (h *GroupHandler) ListGroupRoleAssignments(ctx context.Context, input *ListGroupRoleAssignmentsInput) (*handlerutil.Out[[]types.GroupRoleAssignment], error) {
	rows, err := h.groupService.ListRoleAssignments(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	dtos := make([]types.GroupRoleAssignment, len(rows))
	for i := range rows {
		dtos[i] = toGroupRoleAssignmentDTO(&rows[i])
	}
	return &handlerutil.Out[[]types.GroupRoleAssignment]{
		Body: types.ApiResponse[[]types.GroupRoleAssignment]{Success: true, Data: dtos},
	}, nil
}

func (h *GroupHandler) SetGroupRoleAssignments(ctx context.Context, input *SetGroupRoleAssignmentsInput) (*handlerutil.Out[[]types.GroupRoleAssignment], error) {
	desired := toGroupRoleAssignmentModels(input.Body.Assignments)
	if err := h.groupService.ReplaceRoleAssignments(ctx, input.ID, SourceManual, desired); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	rows, err := h.groupService.ListRoleAssignments(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read back group role assignments: " + err.Error())
	}
	dtos := make([]types.GroupRoleAssignment, len(rows))
	for i := range rows {
		dtos[i] = toGroupRoleAssignmentDTO(&rows[i])
	}
	return &handlerutil.Out[[]types.GroupRoleAssignment]{
		Body: types.ApiResponse[[]types.GroupRoleAssignment]{Success: true, Data: dtos},
	}, nil
}

func toGroupRoleAssignmentModels(inputs []types.UserRoleAssignmentInput) []GroupRoleAssignment {
	desired := make([]GroupRoleAssignment, len(inputs))
	for i, a := range inputs {
		desired[i] = GroupRoleAssignment{RoleID: a.RoleID, EnvironmentID: a.EnvironmentID}
	}
	return desired
}

func (h *GroupHandler) toGroupDTO(ctx context.Context, g *Group) (types.Group, error) {
	memberCount, err := h.groupService.CountMembers(ctx, g.ID)
	if err != nil {
		return types.Group{}, err
	}
	roleCount, err := h.groupService.CountRoleAssignments(ctx, g.ID)
	if err != nil {
		return types.Group{}, err
	}
	//exhaustruct:enforce
	return types.Group{
		ID:                  g.ID,
		Name:                g.Name,
		Description:         g.Description,
		MemberUserIDs:       nil,
		RoleAssignments:     nil,
		MemberCount:         memberCount,
		RoleAssignmentCount: roleCount,
		CreatedAt:           g.CreatedAt,
		UpdatedAt:           g.UpdatedAt,
	}, nil
}

func toGroupMemberDTO(m *GroupMemberDetail) types.GroupMember {
	//exhaustruct:enforce
	return types.GroupMember{
		ID:          m.ID,
		GroupID:     m.GroupID,
		UserID:      m.UserID,
		Username:    m.Username,
		DisplayName: m.DisplayName,
		Email:       m.Email,
		Source:      m.Source,
		CreatedAt:   m.CreatedAt,
	}
}

func toGroupRoleAssignmentDTO(r *GroupRoleAssignment) types.GroupRoleAssignment {
	//exhaustruct:enforce
	return types.GroupRoleAssignment{
		ID:            r.ID,
		GroupID:       r.GroupID,
		RoleID:        r.RoleID,
		EnvironmentID: r.EnvironmentID,
		Source:        r.Source,
		CreatedAt:     r.CreatedAt,
	}
}
