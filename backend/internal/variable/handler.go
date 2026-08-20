package variable

import (
	"context"
	"net/http"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"
)

// VariableHandler handles manager-level global variables and the separate
// agent-only materialization channel used to push effective values.
type VariableHandler struct {
	variableService *VariableService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
}

// ============================================================================
// Input/Output Types
// ============================================================================

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

// ============================================================================
// Route Registration
// ============================================================================

func RegisterVariables(api huma.API, variableService *VariableService, environmentService *environment.EnvironmentService) {
	h := &VariableHandler{variableService: variableService, proxyRemoteJSON: environmentService.ProxyJSONRequest}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listVariables",
		Method:      "GET",
		Path:        "/variables",
		Summary:     "List global variables",
		Description: "List all global variables with their environment scope (secret values are redacted)",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesRead, h.ListVariables)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createVariable",
		Method:      "POST",
		Path:        "/variables",
		Summary:     "Create a global variable",
		Description: "Create a global variable scoped to all or specific environments",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesCreate, h.CreateVariable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateVariable",
		Method:      "PUT",
		Path:        "/variables/{id}",
		Summary:     "Update a global variable",
		Description: "Update a global variable's key, value, secret flag, or environment scope",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesUpdate, h.UpdateVariable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteVariable",
		Method:      "DELETE",
		Path:        "/variables/{id}",
		Summary:     "Delete a global variable",
		Description: "Delete a global variable and re-sync affected environments",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesDelete, h.DeleteVariable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncVariables",
		Method:      "POST",
		Path:        "/variables/sync",
		Summary:     "Sync global variables",
		Description: "Push the effective global variable set to every environment now",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesSync, h.SyncVariables)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getVariableSyncStatus",
		Method:      "GET",
		Path:        "/variables/sync-status",
		Summary:     "Get variable sync status",
		Description: "Get the last global-variable sync result per environment",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesRead, h.GetSyncStatus)
}

// RegisterMaterializedVariables registers the plaintext synchronization
// channel on agents. It must never be registered on a manager API because its
// response intentionally contains decrypted values for .env.global.
func RegisterMaterializedVariables(api huma.API, variableService *VariableService, environmentService *environment.EnvironmentService) {
	h := &VariableHandler{variableService: variableService, proxyRemoteJSON: environmentService.ProxyJSONRequest}

	huma.Register(api, huma.Operation{
		OperationID: "getGlobalVariables",
		Method:      "GET",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Get materialized variables",
		Description: "Get the materialized variable set for an environment. Managed via /variables on the manager.",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireSudo(api),
	}, h.GetMaterializedVariables)

	huma.Register(api, huma.Operation{
		OperationID: "updateGlobalVariables",
		Method:      "PUT",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Update materialized variables",
		Description: "Replace the materialized variable set for an environment. Managed via /variables on the manager.",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireSudo(api),
	}, h.UpdateMaterializedVariables)
}

// ============================================================================
// Handler Methods
// ============================================================================

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
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to retrieve global variables").Error())
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
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to update global variables").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Global variables updated successfully",
			},
		},
	}, nil
}

func variableMutationHTTPErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrInvalidEnvKey), errors.Is(err, common.ErrGlobalVariableSecretValueRequired), errors.Is(err, common.ErrGlobalVariableScopeRequired):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, common.ErrGlobalVariableNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrGlobalVariableConflict):
		return huma.Error409Conflict(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}
