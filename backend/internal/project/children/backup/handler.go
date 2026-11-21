package backup

import (
	"cmp"
	"context"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service *Service
		appCtx  context.Context
	}

	ListInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
		Start         int    `query:"start" default:"0" doc:"Start index"`
		Limit         int    `query:"limit" default:"20" doc:"Limit"`
		IncludeTotal  bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
	}

	CreateInput struct {
		Body          *types.CreateProjectBackupRequest `json:"body,omitempty"`
		EnvironmentID string                            `path:"id" doc:"Environment ID"`
		ProjectID     string                            `path:"projectId" doc:"Project ID"`
	}

	BackupInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
	}

	PoliciesInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
	}

	UpdatePoliciesInput struct {
		EnvironmentID string                            `path:"id" doc:"Environment ID"`
		ProjectID     string                            `path:"projectId" doc:"Project ID"`
		Body          types.UpdateProjectBackupPolicies `doc:"Scheduled project backup policies"`
	}

	UpdateGlobalPoliciesInput struct {
		Body types.UpdateGlobalProjectBackupPolicies `doc:"Global project backup policies"`
	}

	RunGlobalInput struct {
		Body types.RunGlobalProjectBackupsRequest
	}

	RestoreInput struct {
		Body          *types.RestoreProjectBackupRequest `json:"body,omitempty"`
		EnvironmentID string                             `path:"id" doc:"Environment ID"`
		ProjectID     string                             `path:"projectId" doc:"Project ID"`
		BackupID      string                             `path:"backupId" doc:"Backup ID"`
	}
)

func NewHandler(service *Service, appCtx context.Context) *Handler {
	return &Handler{service: service, appCtx: appCtx}
}

func (h *Handler) List(ctx context.Context, input *ListInput) (*handlerutil.Page[types.ProjectBackup], error) {
	backups, page, err := h.service.List(ctx, input.ProjectID, pagination.Params{Start: input.Start, Limit: cmp.Or(input.Limit, 20), SkipCount: !input.IncludeTotal})
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Page[types.ProjectBackup]{
		Body: types.Paginated[types.ProjectBackup]{Success: true, Data: backups, Pagination: handlerutil.PaginationResponse(page)},
	}, nil
}

func (h *Handler) Create(ctx context.Context, input *CreateInput) (*handlerutil.Out[types.ProjectBackup], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := h.service.StartBackup(activity.ActivityRuntimeContext(ctx, h.appCtx), StartOptions{
		User:          *user,
		EnvironmentID: input.EnvironmentID,
		ProjectID:     input.ProjectID,
		Request:       kit.FromPtr(input.Body),
	})
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.ProjectBackup]{Body: types.ApiResponse[types.ProjectBackup]{Success: true, Data: entry}}, nil
}

func (h *Handler) Get(ctx context.Context, input *BackupInput) (*handlerutil.Out[types.ProjectBackup], error) {
	entry, err := h.service.Get(ctx, input.ProjectID, input.BackupID)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.ProjectBackup]{Body: types.ApiResponse[types.ProjectBackup]{Success: true, Data: entry}}, nil
}

func (h *Handler) Delete(ctx context.Context, input *BackupInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.Delete(ctx, input.ProjectID, input.BackupID); err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return handlerutil.MessageOutput("Project backup deleted successfully", ""), nil
}

func (h *Handler) Restore(ctx context.Context, input *RestoreInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	activityID, err := h.service.StartRestore(activity.ActivityRuntimeContext(ctx, h.appCtx), StartRestoreOptions{
		User:          *user,
		EnvironmentID: input.EnvironmentID,
		ProjectID:     input.ProjectID,
		BackupID:      input.BackupID,
		Request:       kit.FromPtr(input.Body),
	})
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return handlerutil.MessageOutput("Project restore started", activityID), nil
}

func (h *Handler) Policies(ctx context.Context, input *PoliciesInput) (*handlerutil.Out[types.ProjectBackupPolicyCollection], error) {
	policies, err := h.service.Policies(ctx, input.ProjectID)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.ProjectBackupPolicyCollection]{Body: types.ApiResponse[types.ProjectBackupPolicyCollection]{Success: true, Data: policies}}, nil
}

func (h *Handler) UpdatePolicies(ctx context.Context, input *UpdatePoliciesInput) (*handlerutil.Out[types.ProjectBackupPolicyCollection], error) {
	policies, err := h.service.UpdatePolicies(ctx, input.ProjectID, input.Body.Policies)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.ProjectBackupPolicyCollection]{Body: types.ApiResponse[types.ProjectBackupPolicyCollection]{Success: true, Data: policies}}, nil
}

func (h *Handler) GlobalPolicies(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.GlobalProjectBackupPolicyCollection], error) {
	policies, err := h.service.GlobalPolicies(ctx)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.GlobalProjectBackupPolicyCollection]{Body: types.ApiResponse[types.GlobalProjectBackupPolicyCollection]{Success: true, Data: policies}}, nil
}

func (h *Handler) UpdateGlobalPolicies(ctx context.Context, input *UpdateGlobalPoliciesInput) (*handlerutil.Out[types.GlobalProjectBackupPolicyCollection], error) {
	policies, err := h.service.UpdateGlobalPolicies(ctx, input.Body.Policies)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.GlobalProjectBackupPolicyCollection]{Body: types.ApiResponse[types.GlobalProjectBackupPolicyCollection]{Success: true, Data: policies}}, nil
}

func (h *Handler) GlobalOptions(ctx context.Context, _ *struct{}) (*handlerutil.Out[[]types.GlobalProjectBackupOption], error) {
	options, err := h.service.GlobalOptions(ctx)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[[]types.GlobalProjectBackupOption]{Body: types.ApiResponse[[]types.GlobalProjectBackupOption]{Success: true, Data: options}}, nil
}

func (h *Handler) RunGlobal(ctx context.Context, input *RunGlobalInput) (*handlerutil.Out[types.BackupRunAccepted], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	accepted, err := h.service.StartGlobal(activity.ActivityRuntimeContext(ctx, h.appCtx), *user, input.Body.PolicyID)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.BackupRunAccepted]{Body: types.ApiResponse[types.BackupRunAccepted]{Success: true, Data: accepted}}, nil
}
