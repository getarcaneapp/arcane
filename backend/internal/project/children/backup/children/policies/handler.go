package policies

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service *Service
		appCtx  context.Context
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
)

func NewHandler(service *Service, appCtx context.Context) *Handler {
	return &Handler{service: service, appCtx: appCtx}
}

func (h *Handler) Policies(ctx context.Context, input *PoliciesInput) (*handlerutil.Out[types.ProjectBackupPolicyCollection], error) {
	policies, err := h.service.Policies(ctx, input.ProjectID)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.ProjectBackupPolicyCollection]{Body: types.ApiResponse[types.ProjectBackupPolicyCollection]{Success: true, Data: policies}}, nil
}

func (h *Handler) UpdatePolicies(ctx context.Context, input *UpdatePoliciesInput) (*handlerutil.Out[types.ProjectBackupPolicyCollection], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	requester := backup.NewResourceRequester(ctx, *user, input.EnvironmentID, authz.PermProjectsBackup, authz.ResourceTypeProject, input.ProjectID)
	policies, err := h.service.UpdatePolicies(ctx, requester, input.ProjectID, input.Body.Policies)
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
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := h.service.UpdateGlobalPolicies(ctx, backup.NewRequester(ctx, *user, "0", authz.PermSystemBackupsUpdate), input.Body.Policies)
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
	accepted, err := h.service.StartGlobal(activity.ActivityRuntimeContext(ctx, h.appCtx), *user, input.Body)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.BackupRunAccepted]{Body: types.ApiResponse[types.BackupRunAccepted]{Success: true, Data: accepted}}, nil
}
