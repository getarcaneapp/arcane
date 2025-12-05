package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/updater"
)

// UpdaterHandler provides Huma-based updater management endpoints.
type UpdaterHandler struct {
	updaterService *services.UpdaterService
}

// --- Huma Input/Output Wrappers ---

type RunUpdaterInput struct {
	EnvironmentID string           `path:"id" doc:"Environment ID"`
	Body          *updater.Options `doc:"Updater run options"`
}

type RunUpdaterOutput struct {
	Body base.ApiResponse[*updater.Result]
}

type UpdateContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ContainerID   string `path:"containerId" doc:"Container ID to update"`
}

type UpdateContainerOutput struct {
	Body base.ApiResponse[*updater.Result]
}

type GetUpdaterStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetUpdaterStatusOutput struct {
	Body base.ApiResponse[updater.Status]
}

type GetUpdaterHistoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Limit         int    `query:"limit" default:"50" doc:"Number of history entries to return"`
}

type GetUpdaterHistoryOutput struct {
	Body base.ApiResponse[[]models.AutoUpdateRecord]
}

// RegisterUpdater registers updater management routes using Huma.
func RegisterUpdater(api huma.API, updaterService *services.UpdaterService) {
	h := &UpdaterHandler{
		updaterService: updaterService,
	}

	huma.Register(api, huma.Operation{
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
	}, h.RunUpdater)

	huma.Register(api, huma.Operation{
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
	}, h.GetUpdaterStatus)

	huma.Register(api, huma.Operation{
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
	}, h.GetUpdaterHistory)

	huma.Register(api, huma.Operation{
		OperationID: "update-container",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{containerId}/update",
		Summary:     "Update a single container",
		Description: "Pull the latest image and recreate a specific container",
		Tags:        []string{"Updater", "Containers"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateContainer)
}

// RunUpdater applies pending container updates.
func (h *UpdaterHandler) RunUpdater(ctx context.Context, input *RunUpdaterInput) (*RunUpdaterOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	dryRun := false
	if input.Body != nil {
		dryRun = input.Body.DryRun
	}

	out, err := h.updaterService.ApplyPending(ctx, dryRun)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdaterRunError{Err: err}).Error())
	}

	return &RunUpdaterOutput{
		Body: base.ApiResponse[*updater.Result]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetUpdaterStatus returns the current status of the updater.
func (h *UpdaterHandler) GetUpdaterStatus(ctx context.Context, input *GetUpdaterStatusInput) (*GetUpdaterStatusOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	status := h.updaterService.GetStatus()

	return &GetUpdaterStatusOutput{
		Body: base.ApiResponse[updater.Status]{
			Success: true,
			Data:    status,
		},
	}, nil
}

// GetUpdaterHistory returns the history of update operations.
func (h *UpdaterHandler) GetUpdaterHistory(ctx context.Context, input *GetUpdaterHistoryInput) (*GetUpdaterHistoryOutput, error) {
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

	return &GetUpdaterHistoryOutput{
		Body: base.ApiResponse[[]models.AutoUpdateRecord]{
			Success: true,
			Data:    history,
		},
	}, nil
}

// UpdateContainer updates a single container by pulling the latest image and recreating it.
func (h *UpdaterHandler) UpdateContainer(ctx context.Context, input *UpdateContainerInput) (*UpdateContainerOutput, error) {
	if h.updaterService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	out, err := h.updaterService.UpdateSingleContainer(ctx, input.ContainerID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.UpdaterRunError{Err: err}).Error())
	}

	return &UpdateContainerOutput{
		Body: base.ApiResponse[*updater.Result]{
			Success: true,
			Data:    out,
		},
	}, nil
}
