package variable

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// VariableHandler handles manager-level global variables and the separate
// agent-only materialization channel used to push effective values.
type VariableHandler struct {
	variableService *VariableService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
}

type ListGlobalVariablesInput struct{}

type CreateGlobalVariableInput struct {
	types.Create[types.GlobalVariable]
}

type UpdateGlobalVariableInput struct {
	ID string `path:"id" doc:"Variable ID"`
	types.Update[types.GlobalVariable]
}

type DeleteGlobalVariableInput struct {
	ID string `path:"id" doc:"Variable ID"`
}

type SyncGlobalVariablesInput struct{}

type GetGlobalVariableSyncStatusInput struct{}

type UpdateGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.EnvSummary
}

func (h *VariableHandler) ListVariables(ctx context.Context, _ *ListGlobalVariablesInput) (*handlerutil.Out[[]types.GlobalVariable], error) {
	variables, err := h.variableService.ListVariables(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[[]types.GlobalVariable]{
		Body: types.ApiResponse[[]types.GlobalVariable]{
			Success: true,
			Data:    variables,
		},
	}, nil
}

func (h *VariableHandler) CreateVariable(ctx context.Context, input *CreateGlobalVariableInput) (*handlerutil.Out[types.GlobalVariableMutationResponse], error) {
	variable, err := h.variableService.CreateVariable(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[types.GlobalVariableMutationResponse]{
		Body: types.ApiResponse[types.GlobalVariableMutationResponse]{
			Success: true,
			Data: types.GlobalVariableMutationResponse{
				Variable:    variable,
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) UpdateVariable(ctx context.Context, input *UpdateGlobalVariableInput) (*handlerutil.Out[types.GlobalVariableMutationResponse], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}

	variable, err := h.variableService.UpdateVariable(ctx, input.ID, input.Body, submitted)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[types.GlobalVariableMutationResponse]{
		Body: types.ApiResponse[types.GlobalVariableMutationResponse]{
			Success: true,
			Data: types.GlobalVariableMutationResponse{
				Variable:    variable,
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) DeleteVariable(ctx context.Context, input *DeleteGlobalVariableInput) (*handlerutil.Out[types.GlobalVariableMutationResponse], error) {
	if err := h.variableService.DeleteVariable(ctx, input.ID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[types.GlobalVariableMutationResponse]{
		Body: types.ApiResponse[types.GlobalVariableMutationResponse]{
			Success: true,
			Data: types.GlobalVariableMutationResponse{
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) SyncVariables(ctx context.Context, _ *SyncGlobalVariablesInput) (*handlerutil.Out[[]types.EnvironmentSyncStatus], error) {
	return &handlerutil.Out[[]types.EnvironmentSyncStatus]{
		Body: types.ApiResponse[[]types.EnvironmentSyncStatus]{
			Success: true,
			Data:    h.variableService.SyncAll(ctx),
		},
	}, nil
}

func (h *VariableHandler) GetSyncStatus(_ context.Context, _ *GetGlobalVariableSyncStatusInput) (*handlerutil.Out[[]types.EnvironmentSyncStatus], error) {
	return &handlerutil.Out[[]types.EnvironmentSyncStatus]{
		Body: types.ApiResponse[[]types.EnvironmentSyncStatus]{
			Success: true,
			Data:    h.variableService.SyncStatuses(),
		},
	}, nil
}

// GetMaterializedVariables returns the environment's materialized .env.global
// content (local file for environment "0", proxied to the agent otherwise).
func (h *VariableHandler) GetMaterializedVariables(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[[]types.EnvVariable], error) {
	if input.EnvironmentID != "0" {
		response, err := h.proxyRemoteJSON.JSON[types.ApiResponse[[]types.EnvVariable]](ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/templates/variables", nil)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[[]types.EnvVariable]{Body: *response}, nil
	}

	vars, err := h.variableService.ReadLocalEnvFile(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve global variables: " + err.Error())
	}

	return &handlerutil.Out[[]types.EnvVariable]{
		Body: types.ApiResponse[[]types.EnvVariable]{
			Success: true,
			Data:    vars,
		},
	}, nil
}

// UpdateMaterializedVariables replaces the environment's materialized
// .env.global content (local file for environment "0", proxied otherwise).
func (h *VariableHandler) UpdateMaterializedVariables(ctx context.Context, input *UpdateGlobalVariablesInput) (*handlerutil.Out[types.MessageResponse], error) {
	if input.EnvironmentID != "0" {
		response, err := h.proxyRemoteJSON.JSON[types.ApiResponse[types.MessageResponse]](ctx, input.EnvironmentID, http.MethodPut, "/api/environments/0/templates/variables", input.Body)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[types.MessageResponse]{Body: *response}, nil
	}

	if err := h.variableService.WriteLocalEnvFile(ctx, input.Body.Variables); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return handlerutil.MessageOutput("Global variables updated successfully", ""), nil
}
