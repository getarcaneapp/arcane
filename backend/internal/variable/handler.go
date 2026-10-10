package variable

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// VariableHandler handles manager-level global variables and the separate
// agent-only materialization channel used to push effective values.
type VariableHandler struct {
	variableService *VariableService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
}

type ListGlobalVariablesInput struct{}

type CreateGlobalVariableInput struct {
	Body env.CreateGlobalVariableRequest
}

type UpdateGlobalVariableInput struct {
	ID   string `path:"id" doc:"Variable ID"`
	Body env.UpdateGlobalVariableRequest
}

type DeleteGlobalVariableInput struct {
	ID string `path:"id" doc:"Variable ID"`
}

type SyncGlobalVariablesInput struct{}

type GetGlobalVariableSyncStatusInput struct{}

type GetGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type UpdateGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          env.Summary
}

func (h *VariableHandler) ListVariables(ctx context.Context, _ *ListGlobalVariablesInput) (*handlerutil.Out[[]env.GlobalVariable], error) {
	variables, err := h.variableService.ListVariables(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[[]env.GlobalVariable]{
		Body: base.ApiResponse[[]env.GlobalVariable]{
			Success: true,
			Data:    variables,
		},
	}, nil
}

func (h *VariableHandler) CreateVariable(ctx context.Context, input *CreateGlobalVariableInput) (*handlerutil.Out[env.GlobalVariableMutationResponse], error) {
	variable, err := h.variableService.CreateVariable(ctx, input.Body)
	if err != nil {
		return nil, variableMutationHTTPErrorInternal(err)
	}

	return &handlerutil.Out[env.GlobalVariableMutationResponse]{
		Body: base.ApiResponse[env.GlobalVariableMutationResponse]{
			Success: true,
			Data: env.GlobalVariableMutationResponse{
				Variable:    variable,
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) UpdateVariable(ctx context.Context, input *UpdateGlobalVariableInput) (*handlerutil.Out[env.GlobalVariableMutationResponse], error) {
	variable, err := h.variableService.UpdateVariable(ctx, input.ID, input.Body)
	if err != nil {
		return nil, variableMutationHTTPErrorInternal(err)
	}

	return &handlerutil.Out[env.GlobalVariableMutationResponse]{
		Body: base.ApiResponse[env.GlobalVariableMutationResponse]{
			Success: true,
			Data: env.GlobalVariableMutationResponse{
				Variable:    variable,
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) DeleteVariable(ctx context.Context, input *DeleteGlobalVariableInput) (*handlerutil.Out[env.GlobalVariableMutationResponse], error) {
	if err := h.variableService.DeleteVariable(ctx, input.ID); err != nil {
		return nil, variableMutationHTTPErrorInternal(err)
	}

	return &handlerutil.Out[env.GlobalVariableMutationResponse]{
		Body: base.ApiResponse[env.GlobalVariableMutationResponse]{
			Success: true,
			Data: env.GlobalVariableMutationResponse{
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) SyncVariables(ctx context.Context, _ *SyncGlobalVariablesInput) (*handlerutil.Out[[]env.EnvironmentSyncStatus], error) {
	return &handlerutil.Out[[]env.EnvironmentSyncStatus]{
		Body: base.ApiResponse[[]env.EnvironmentSyncStatus]{
			Success: true,
			Data:    h.variableService.SyncAll(ctx),
		},
	}, nil
}

func (h *VariableHandler) GetSyncStatus(_ context.Context, _ *GetGlobalVariableSyncStatusInput) (*handlerutil.Out[[]env.EnvironmentSyncStatus], error) {
	return &handlerutil.Out[[]env.EnvironmentSyncStatus]{
		Body: base.ApiResponse[[]env.EnvironmentSyncStatus]{
			Success: true,
			Data:    h.variableService.SyncStatuses(),
		},
	}, nil
}

// GetMaterializedVariables returns the environment's materialized .env.global
// content (local file for environment "0", proxied to the agent otherwise).
func (h *VariableHandler) GetMaterializedVariables(ctx context.Context, input *GetGlobalVariablesInput) (*handlerutil.Out[[]env.Variable], error) {
	if input.EnvironmentID != "0" {
		response, err := h.proxyRemoteJSON.JSON[base.ApiResponse[[]env.Variable]](ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/templates/variables", nil)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[[]env.Variable]{Body: *response}, nil
	}

	vars, err := h.variableService.ReadLocalEnvFile(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve global variables: " + err.Error())
	}

	return &handlerutil.Out[[]env.Variable]{
		Body: base.ApiResponse[[]env.Variable]{
			Success: true,
			Data:    vars,
		},
	}, nil
}

// UpdateMaterializedVariables replaces the environment's materialized
// .env.global content (local file for environment "0", proxied otherwise).
func (h *VariableHandler) UpdateMaterializedVariables(ctx context.Context, input *UpdateGlobalVariablesInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.EnvironmentID != "0" {
		response, err := h.proxyRemoteJSON.JSON[base.ApiResponse[base.MessageResponse]](ctx, input.EnvironmentID, http.MethodPut, "/api/environments/0/templates/variables", input.Body)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[base.MessageResponse]{Body: *response}, nil
	}

	if err := h.variableService.WriteLocalEnvFile(ctx, input.Body.Variables); err != nil {
		if errors.Is(err, common.ErrInvalidEnvKey) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to update global variables: " + err.Error())
	}

	return handlerutil.MessageOutput("Global variables updated successfully", ""), nil
}

func variableMutationHTTPErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrInvalidEnvKey),
		errors.Is(err, common.ErrGlobalVariableSecretValueRequired),
		errors.Is(err, common.ErrGlobalVariableScopeRequired):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, common.ErrGlobalVariableNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrGlobalVariableConflict):
		return huma.Error409Conflict(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}
