package updater

import (
	"cmp"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// UpdaterHandler provides Huma-based updater management endpoints.
type UpdaterHandler struct {
	updaterService *UpdaterService
	appCtx         context.Context
}

type RunUpdaterInput struct {
	EnvironmentID string                `path:"id" doc:"Environment ID"`
	Body          *types.UpdaterOptions `doc:"Updater run options"`
}

type UpdateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID to update"`
	Async         bool   `query:"async" doc:"Return an accepted activity immediately instead of waiting for the update"`
}

type updateContainerOutput struct {
	Status int
	Body   types.ApiResponse[any]
}

type GetUpdaterStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetUpdaterHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Limit         int    `query:"limit" default:"50" doc:"Number of history entries to return"`
}

// RunUpdater applies pending container updates.
func (h *UpdaterHandler) RunUpdater(ctx context.Context, input *RunUpdaterInput) (*handlerutil.Out[*types.UpdaterResult], error) {
	out, err := h.updaterService.ApplyPending(utils.ActivityRuntimeContext(ctx, h.appCtx), kit.FromPtr(input.Body))
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to run updater: "+apiErr.Message)
	}

	return &handlerutil.Out[*types.UpdaterResult]{
		Body: types.ApiResponse[*types.UpdaterResult]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUpdaterStatus returns the current status of the updater.
func (h *UpdaterHandler) GetUpdaterStatus(ctx context.Context, input *GetUpdaterStatusInput) (*handlerutil.Out[types.UpdaterStatus], error) {
	return &handlerutil.Out[types.UpdaterStatus]{
		Body: types.ApiResponse[types.UpdaterStatus]{
			Success: true,
			Data:    h.updaterService.GetStatus(),
		},
	}, nil
}

// GetUpdaterHistory returns the history of update operations.
func (h *UpdaterHandler) GetUpdaterHistory(ctx context.Context, input *GetUpdaterHistoryInput) (*handlerutil.Out[[]types.AutoUpdateRecord], error) {
	history, err := h.updaterService.GetHistory(ctx, cmp.Or(max(input.Limit, 0), 50))
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get updater history: " + err.Error())
	}

	records, err := mapping.MapSlice[AutoUpdateRecord, types.AutoUpdateRecord](history)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get updater history: " + err.Error())
	}

	return &handlerutil.Out[[]types.AutoUpdateRecord]{
		Body: types.ApiResponse[[]types.AutoUpdateRecord]{
			Success: true,
			Data:    records,
		},
	}, nil
}

func (h *UpdaterHandler) updateContainer(ctx context.Context, input *UpdateContainerInput) (*updateContainerOutput, error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	if input.Async {
		acceptedActivity, err := h.updaterService.AcceptSingleContainerUpdate(runtimeCtx, input.ContainerID)
		if err != nil {
			return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), "Failed to accept container update: "+err.Error())
		}
		return &updateContainerOutput{
			Status: http.StatusAccepted,
			Body: types.ApiResponse[any]{
				Success: true,
				Data:    acceptedActivity,
			},
		}, nil
	}
	out, err := h.updaterService.UpdateSingleContainer(runtimeCtx, input.ContainerID)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), "Failed to run updater: "+err.Error())
	}

	return &updateContainerOutput{
		Status: http.StatusOK,
		Body: types.ApiResponse[any]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// CheckProjectUpdates checks Compose policies through the updater service.
func (h *UpdaterHandler) CheckProjectUpdates(ctx context.Context, input *types.UpdaterCheckProjectInput) (*handlerutil.Out[*types.ProjectUpdateInfo], error) {
	result, err := h.updaterService.CheckProjectUpdates(ctx, input.ProjectID)
	if err != nil {
		return nil, err
	}
	return &handlerutil.Out[*types.ProjectUpdateInfo]{
		Body: types.ApiResponse[*types.ProjectUpdateInfo]{
			Success: true,
			Data:    result,
		},
	}, nil
}
