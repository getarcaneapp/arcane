package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/updater"
)

// updaterHandler provides Huma-based updater management endpoints.
type updaterHandler struct {
	updaterService *services.UpdaterService
	appCtx         context.Context
}

// --- Huma Input/Output Wrappers ---

type runUpdaterInput struct {
	EnvironmentID string           `path:"id" doc:"Environment ID"`
	Body          *updater.Options `doc:"Updater run options"`
}

type runUpdaterOutput struct {
	Body base.ApiResponse[*updater.Result]
}

type updateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID to update"`
}

type updateContainerOutput struct {
	Body base.ApiResponse[*updater.Result]
}

type getUpdaterStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type getUpdaterStatusOutput struct {
	Body base.ApiResponse[updater.Status]
}

type getUpdaterHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Limit         int    `query:"limit" default:"50" doc:"Number of history entries to return"`
}

type getUpdaterHistoryOutput struct {
	Body base.ApiResponse[[]models.AutoUpdateRecord]
}

// RegisterUpdater registers updater management routes using Huma.
func RegisterUpdater(api huma.API, updaterService *services.UpdaterService, appCtx ActivityAppContext) {
	h := &updaterHandler{
		updaterService: updaterService,
		appCtx:         appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "run-updater",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/updater/run",
		Summary:     "Run updater",
		Description: "Apply pending container updates",
		Tags:        []string{"Updater"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImageUpdatesCheck, h.runUpdaterInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-status",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/updater/status",
		Summary:     "Get updater status",
		Description: "Get the current status of the updater",
		Tags:        []string{"Updater"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImageUpdatesRead, h.getUpdaterStatusInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-updater-history",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/updater/history",
		Summary:     "Get updater history",
		Description: "Get the history of update operations",
		Tags:        []string{"Updater"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImageUpdatesRead, h.getUpdaterHistoryInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/update",
		Summary:     "Update a single container",
		Description: "Pull the latest image and apply the appropriate update strategy for a specific container",
		Tags:        []string{"Updater", "Containers"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermImageUpdatesCheck, h.updateContainerInternal)
}

// RunUpdater applies pending container updates.
func (h *updaterHandler) runUpdaterInternal(ctx context.Context, input *runUpdaterInput) (*runUpdaterOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	options := updater.Options{}
	if input.Body != nil {
		options = *input.Body
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	out, err := h.updaterService.ApplyPending(runtimeCtx, options)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdaterRunError{Err: err}).Error())
	}

	return &runUpdaterOutput{
		Body: base.ApiResponse[*updater.Result]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUpdaterStatus returns the current status of the updater.
func (h *updaterHandler) getUpdaterStatusInternal(_ context.Context, _ *getUpdaterStatusInput) (*getUpdaterStatusOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	status := h.updaterService.GetStatus()

	return &getUpdaterStatusOutput{
		Body: base.ApiResponse[updater.Status]{
			Success: true,
			Data:    status,
		},
	}, nil
}

// GetUpdaterHistory returns the history of update operations.
func (h *updaterHandler) getUpdaterHistoryInternal(ctx context.Context, input *getUpdaterHistoryInput) (*getUpdaterHistoryOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}

	history, err := h.updaterService.GetHistory(ctx, limit)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdaterHistoryError{Err: err}).Error())
	}

	return &getUpdaterHistoryOutput{
		Body: base.ApiResponse[[]models.AutoUpdateRecord]{
			Success: true,
			Data:    history,
		},
	}, nil
}

// UpdateContainer updates a single container by pulling the latest image and applying the appropriate update flow.
func (h *updaterHandler) updateContainerInternal(ctx context.Context, input *updateContainerInput) (*updateContainerOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	out, err := h.updaterService.UpdateSingleContainer(runtimeCtx, input.ContainerID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdaterRunError{Err: err}).Error())
	}

	return &updateContainerOutput{
		Body: base.ApiResponse[*updater.Result]{
			Success: true,
			Data:    out,
		},
	}, nil
}
