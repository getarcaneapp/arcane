package updater

import (
	"context"
	"net/http"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/updater"
)

// UpdaterHandler provides Huma-based updater management endpoints.
type UpdaterHandler struct {
	updaterService *UpdaterService
	appCtx         context.Context
}

// --- Huma Input/Output Wrappers ---

type RunUpdaterInput struct {
	EnvironmentID string           `path:"id" doc:"Environment ID"`
	Body          *updater.Options `doc:"Updater run options"`
}

type UpdateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID to update"`
}

type GetUpdaterStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetUpdaterHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Limit         int    `query:"limit" default:"50" doc:"Number of history entries to return"`
}

// RegisterUpdater registers updater management routes using Huma.
func RegisterUpdater(api huma.API, updaterService *UpdaterService, appCtx handlerutil.ActivityAppContext) {
	h := &UpdaterHandler{
		updaterService: updaterService,
		appCtx:         appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "run-updater",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/updater/run",
		Summary:     "Run updater",
		Description: "Apply pending container updates",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.RunUpdater)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-status",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/updater/status",
		Summary:     "Get updater status",
		Description: "Get the current status of the updater",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdaterStatus)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-history",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/updater/history",
		Summary:     "Get updater history",
		Description: "Get the history of update operations",
		Tags:        []string{"Updater"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdaterHistory)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/update",
		Summary:     "Update a single container",
		Description: "Pull the latest image and apply the appropriate update strategy for a specific container",
		Tags:        []string{"Updater", "Containers"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.UpdateContainer)
}

// RunUpdater applies pending container updates.
func (h *UpdaterHandler) RunUpdater(ctx context.Context, input *RunUpdaterInput) (*handlerutil.Out[*updater.Result], error) {
	options := updater.Options{}
	if input.Body != nil {
		options = *input.Body
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	out, err := h.updaterService.ApplyPending(runtimeCtx, options)
	if err != nil {
		if errors.Is(err, common.ErrBadRequest) {
			return nil, huma.Error400BadRequest(errors.WithMessage(err, "Failed to run updater").Error())
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to run updater").Error())
	}

	return &handlerutil.Out[*updater.Result]{
		Body: base.ApiResponse[*updater.Result]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUpdaterStatus returns the current status of the updater.
func (h *UpdaterHandler) GetUpdaterStatus(ctx context.Context, input *GetUpdaterStatusInput) (*handlerutil.Out[updater.Status], error) {
	status := h.updaterService.GetStatus()

	return &handlerutil.Out[updater.Status]{
		Body: base.ApiResponse[updater.Status]{
			Success: true,
			Data:    status,
		},
	}, nil
}

// GetUpdaterHistory returns the history of update operations.
func (h *UpdaterHandler) GetUpdaterHistory(ctx context.Context, input *GetUpdaterHistoryInput) (*handlerutil.Out[[]AutoUpdateRecord], error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}

	history, err := h.updaterService.GetHistory(ctx, limit)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get updater history").Error())
	}

	return &handlerutil.Out[[]AutoUpdateRecord]{
		Body: base.ApiResponse[[]AutoUpdateRecord]{
			Success: true,
			Data:    history,
		},
	}, nil
}

// UpdateContainer updates a single container by pulling the latest image and applying the appropriate update flow.
func (h *UpdaterHandler) UpdateContainer(ctx context.Context, input *UpdateContainerInput) (*handlerutil.Out[*updater.Result], error) {
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	out, err := h.updaterService.UpdateSingleContainer(runtimeCtx, input.ContainerID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to run updater").Error())
	}

	return &handlerutil.Out[*updater.Result]{
		Body: base.ApiResponse[*updater.Result]{
			Success: true,
			Data:    out,
		},
	}, nil
}
