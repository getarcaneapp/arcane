package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"
)

// VariableHandler handles the manager-level global variable endpoints plus the
// per-environment /environments/{id}/templates/variables routes, which remain
// the materialization channel the manager pushes through to agents.
type VariableHandler struct {
	variableService    *services.VariableService
	environmentService *services.EnvironmentService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListGlobalVariablesInput struct{}

type ListGlobalVariablesOutput struct {
	Body base.ApiResponse[[]env.GlobalVariable]
}

type CreateGlobalVariableInput struct {
	Body env.CreateGlobalVariableRequest
}

type CreateGlobalVariableOutput struct {
	Body base.ApiResponse[env.GlobalVariableMutationResponse]
}

type UpdateGlobalVariableInput struct {
	ID   string `path:"id" doc:"Variable ID"`
	Body env.UpdateGlobalVariableRequest
}

type UpdateGlobalVariableOutput struct {
	Body base.ApiResponse[env.GlobalVariableMutationResponse]
}

type DeleteGlobalVariableInput struct {
	ID string `path:"id" doc:"Variable ID"`
}

type DeleteGlobalVariableOutput struct {
	Body base.ApiResponse[env.GlobalVariableMutationResponse]
}

type SyncGlobalVariablesInput struct{}

type SyncGlobalVariablesOutput struct {
	Body base.ApiResponse[[]env.EnvironmentSyncStatus]
}

type GetGlobalVariableSyncStatusInput struct{}

type GetGlobalVariableSyncStatusOutput struct {
	Body base.ApiResponse[[]env.EnvironmentSyncStatus]
}

type GetGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetGlobalVariablesOutput struct {
	Body base.ApiResponse[[]env.Variable]
}

type UpdateGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          env.Summary
}

type UpdateGlobalVariablesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Route Registration
// ============================================================================

func RegisterVariables(api huma.API, variableService *services.VariableService, environmentService *services.EnvironmentService) {
	h := &VariableHandler{variableService: variableService, environmentService: environmentService}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "listVariables",
		Method:      "GET",
		Path:        "/variables",
		Summary:     "List global variables",
		Description: "List all global variables with their environment scope (secret values are redacted)",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesRead, h.ListVariables)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "createVariable",
		Method:      "POST",
		Path:        "/variables",
		Summary:     "Create a global variable",
		Description: "Create a global variable scoped to all or specific environments",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesUpdate, h.CreateVariable)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateVariable",
		Method:      "PUT",
		Path:        "/variables/{id}",
		Summary:     "Update a global variable",
		Description: "Update a global variable's key, value, secret flag, or environment scope",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesUpdate, h.UpdateVariable)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteVariable",
		Method:      "DELETE",
		Path:        "/variables/{id}",
		Summary:     "Delete a global variable",
		Description: "Delete a global variable and re-sync affected environments",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesUpdate, h.DeleteVariable)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncVariables",
		Method:      "POST",
		Path:        "/variables/sync",
		Summary:     "Sync global variables",
		Description: "Push the effective global variable set to every environment now",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesUpdate, h.SyncVariables)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "getVariableSyncStatus",
		Method:      "GET",
		Path:        "/variables/sync-status",
		Summary:     "Get variable sync status",
		Description: "Get the last global-variable sync result per environment",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesRead, h.GetSyncStatus)

	// Per-environment materialized-file routes. Agents serve these locally; the
	// manager pushes each environment's effective set through them.
	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "getGlobalVariables",
		Method:      "GET",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Get materialized variables",
		Description: "Get the materialized variable set for an environment. Managed via /variables on the manager.",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesRead, h.GetMaterializedVariables)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateGlobalVariables",
		Method:      "PUT",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Update materialized variables",
		Description: "Replace the materialized variable set for an environment. Managed via /variables on the manager.",
		Tags:        []string{"Variables"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesUpdate, h.UpdateMaterializedVariables)
}

// ============================================================================
// Handler Methods
// ============================================================================

func (h *VariableHandler) ListVariables(ctx context.Context, _ *ListGlobalVariablesInput) (*ListGlobalVariablesOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	variables, err := h.variableService.ListVariables(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &ListGlobalVariablesOutput{
		Body: base.ApiResponse[[]env.GlobalVariable]{
			Success: true,
			Data:    variables,
		},
	}, nil
}

func (h *VariableHandler) CreateVariable(ctx context.Context, input *CreateGlobalVariableInput) (*CreateGlobalVariableOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	variable, err := h.variableService.CreateVariable(ctx, input.Body)
	if err != nil {
		return nil, variableMutationHTTPErrorInternal(err)
	}

	return &CreateGlobalVariableOutput{
		Body: base.ApiResponse[env.GlobalVariableMutationResponse]{
			Success: true,
			Data: env.GlobalVariableMutationResponse{
				Variable:    variable,
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) UpdateVariable(ctx context.Context, input *UpdateGlobalVariableInput) (*UpdateGlobalVariableOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	variable, err := h.variableService.UpdateVariable(ctx, input.ID, input.Body)
	if err != nil {
		return nil, variableMutationHTTPErrorInternal(err)
	}

	return &UpdateGlobalVariableOutput{
		Body: base.ApiResponse[env.GlobalVariableMutationResponse]{
			Success: true,
			Data: env.GlobalVariableMutationResponse{
				Variable:    variable,
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) DeleteVariable(ctx context.Context, input *DeleteGlobalVariableInput) (*DeleteGlobalVariableOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.variableService.DeleteVariable(ctx, input.ID); err != nil {
		return nil, variableMutationHTTPErrorInternal(err)
	}

	return &DeleteGlobalVariableOutput{
		Body: base.ApiResponse[env.GlobalVariableMutationResponse]{
			Success: true,
			Data: env.GlobalVariableMutationResponse{
				SyncResults: h.variableService.SyncAllBackground(ctx),
			},
		},
	}, nil
}

func (h *VariableHandler) SyncVariables(ctx context.Context, _ *SyncGlobalVariablesInput) (*SyncGlobalVariablesOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return &SyncGlobalVariablesOutput{
		Body: base.ApiResponse[[]env.EnvironmentSyncStatus]{
			Success: true,
			Data:    h.variableService.SyncAll(ctx),
		},
	}, nil
}

func (h *VariableHandler) GetSyncStatus(_ context.Context, _ *GetGlobalVariableSyncStatusInput) (*GetGlobalVariableSyncStatusOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	return &GetGlobalVariableSyncStatusOutput{
		Body: base.ApiResponse[[]env.EnvironmentSyncStatus]{
			Success: true,
			Data:    h.variableService.SyncStatuses(),
		},
	}, nil
}

// GetMaterializedVariables returns the environment's materialized .env.global
// content (local file for environment "0", proxied to the agent otherwise).
func (h *VariableHandler) GetMaterializedVariables(ctx context.Context, input *GetGlobalVariablesInput) (*GetGlobalVariablesOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		response, err := proxyRemoteJSONInternal[base.ApiResponse[[]env.Variable]](ctx, h.environmentService, input.EnvironmentID, http.MethodGet, "/api/environments/0/templates/variables", nil)
		if err != nil {
			return nil, err
		}
		return &GetGlobalVariablesOutput{Body: *response}, nil
	}

	vars, err := h.variableService.ReadLocalEnvFile(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GlobalVariablesRetrievalError{Err: err}).Error())
	}

	return &GetGlobalVariablesOutput{
		Body: base.ApiResponse[[]env.Variable]{
			Success: true,
			Data:    vars,
		},
	}, nil
}

// UpdateMaterializedVariables replaces the environment's materialized
// .env.global content (local file for environment "0", proxied otherwise).
func (h *VariableHandler) UpdateMaterializedVariables(ctx context.Context, input *UpdateGlobalVariablesInput) (*UpdateGlobalVariablesOutput, error) {
	if h.variableService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		response, err := proxyRemoteJSONInternal[base.ApiResponse[base.MessageResponse]](ctx, h.environmentService, input.EnvironmentID, http.MethodPut, "/api/environments/0/templates/variables", input.Body)
		if err != nil {
			return nil, err
		}
		return &UpdateGlobalVariablesOutput{Body: *response}, nil
	}

	if err := h.variableService.WriteLocalEnvFile(ctx, input.Body.Variables); err != nil {
		if common.IsInvalidEnvKeyError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.GlobalVariablesUpdateError{Err: err}).Error())
	}

	return &UpdateGlobalVariablesOutput{
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
	case common.IsInvalidEnvKeyError(err), common.IsGlobalVariableSecretValueRequiredError(err), common.IsGlobalVariableScopeRequiredError(err):
		return huma.Error400BadRequest(err.Error())
	case common.IsGlobalVariableNotFoundError(err):
		return huma.Error404NotFound(err.Error())
	case common.IsGlobalVariableConflictError(err):
		return huma.Error409Conflict(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}
