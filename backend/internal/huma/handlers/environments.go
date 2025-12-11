package handlers

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/environment"
)

const localDockerEnvironmentID = "0"

// EnvironmentHandler handles environment management endpoints.
type EnvironmentHandler struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	cfg                *config.Config
}

// ============================================================================
// Input/Output Types
// ============================================================================

// EnvironmentPaginatedResponse is the paginated response for environments.
type EnvironmentPaginatedResponse struct {
	Success    bool                      `json:"success"`
	Data       []environment.Environment `json:"data"`
	Pagination base.PaginationResponse   `json:"pagination"`
}

type ListEnvironmentsInput struct {
	Page    int    `query:"pagination[page]" default:"1" doc:"Page number"`
	Limit   int    `query:"pagination[limit]" default:"20" doc:"Items per page"`
	SortCol string `query:"sort[column]" doc:"Column to sort by"`
	SortDir string `query:"sort[direction]" default:"asc" doc:"Sort direction"`
}

type ListEnvironmentsOutput struct {
	Body EnvironmentPaginatedResponse
}

type CreateEnvironmentInput struct {
	Body environment.Create
}

type CreateEnvironmentOutput struct {
	Body base.ApiResponse[environment.Environment]
}

type GetEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentOutput struct {
	Body base.ApiResponse[environment.Environment]
}

type UpdateEnvironmentInput struct {
	ID   string `path:"id" doc:"Environment ID"`
	Body environment.Update
}

type UpdateEnvironmentOutput struct {
	Body base.ApiResponse[environment.Environment]
}

type DeleteEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type DeleteEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type TestConnectionInput struct {
	ID   string                             `path:"id" doc:"Environment ID"`
	Body *environment.TestConnectionRequest `json:"body,omitempty"`
}

type TestConnectionOutput struct {
	Body base.ApiResponse[environment.Test]
}

type UpdateHeartbeatInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type UpdateHeartbeatOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PairAgentInput struct {
	ID   string                        `path:"id" doc:"Environment ID (must be 0 for local)"`
	Body *environment.AgentPairRequest `json:"body,omitempty"`
}

type PairAgentOutput struct {
	Body base.ApiResponse[environment.AgentPairResponse]
}

type SyncRegistriesInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type SyncRegistriesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterEnvironments registers all environment management endpoints.
func RegisterEnvironments(api huma.API, environmentService *services.EnvironmentService, settingsService *services.SettingsService, cfg *config.Config) {
	h := &EnvironmentHandler{
		environmentService: environmentService,
		settingsService:    settingsService,
		cfg:                cfg,
	}

	huma.Register(api, huma.Operation{
		OperationID: "listEnvironments",
		Method:      "GET",
		Path:        "/environments",
		Summary:     "List environments",
		Description: "Get a paginated list of Docker environments",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListEnvironments)

	huma.Register(api, huma.Operation{
		OperationID: "createEnvironment",
		Method:      "POST",
		Path:        "/environments",
		Summary:     "Create an environment",
		Description: "Create a new Docker environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "getEnvironment",
		Method:      "GET",
		Path:        "/environments/{id}",
		Summary:     "Get an environment",
		Description: "Get a Docker environment by ID",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "updateEnvironment",
		Method:      "PUT",
		Path:        "/environments/{id}",
		Summary:     "Update an environment",
		Description: "Update a Docker environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEnvironment",
		Method:      "DELETE",
		Path:        "/environments/{id}",
		Summary:     "Delete an environment",
		Description: "Delete a Docker environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "testConnection",
		Method:      "POST",
		Path:        "/environments/{id}/test",
		Summary:     "Test environment connection",
		Description: "Test connectivity to a Docker environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.TestConnection)

	huma.Register(api, huma.Operation{
		OperationID: "updateHeartbeat",
		Method:      "POST",
		Path:        "/environments/{id}/heartbeat",
		Summary:     "Update environment heartbeat",
		Description: "Update the heartbeat timestamp for an environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateHeartbeat)

	huma.Register(api, huma.Operation{
		OperationID: "pairAgent",
		Method:      "POST",
		Path:        "/environments/{id}/agent/pair",
		Summary:     "Pair with local agent",
		Description: "Generate or rotate the local agent pairing token",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.PairAgent)

	huma.Register(api, huma.Operation{
		OperationID: "syncEnvironmentRegistries",
		Method:      "POST",
		Path:        "/environments/{id}/sync-registries",
		Summary:     "Sync container registries",
		Description: "Sync container registries to a remote environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.SyncRegistries)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListEnvironments returns a paginated list of environments.
func (h *EnvironmentHandler) ListEnvironments(ctx context.Context, input *ListEnvironmentsInput) (*ListEnvironmentsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Limit, input.SortCol, input.SortDir)

	envs, paginationResp, err := h.environmentService.ListEnvironmentsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentListError{Err: err}).Error())
	}

	return &ListEnvironmentsOutput{
		Body: EnvironmentPaginatedResponse{
			Success: true,
			Data:    envs,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// CreateEnvironment creates a new environment.
func (h *EnvironmentHandler) CreateEnvironment(ctx context.Context, input *CreateEnvironmentInput) (*CreateEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	env := &models.Environment{
		ApiUrl:  input.Body.ApiUrl,
		Enabled: true,
	}
	if input.Body.Name != nil {
		env.Name = *input.Body.Name
	}
	if input.Body.Enabled != nil {
		env.Enabled = *input.Body.Enabled
	}

	if (input.Body.AccessToken == nil || *input.Body.AccessToken == "") && input.Body.BootstrapToken != nil && *input.Body.BootstrapToken != "" {
		token, err := h.environmentService.PairAgentWithBootstrap(ctx, input.Body.ApiUrl, *input.Body.BootstrapToken)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to pair with agent", "apiUrl", input.Body.ApiUrl, "error", err.Error())
			return nil, huma.Error502BadGateway((&common.AgentPairingError{Err: err}).Error())
		}
		env.AccessToken = &token
	} else if input.Body.AccessToken != nil && *input.Body.AccessToken != "" {
		env.AccessToken = input.Body.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(ctx, env)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentCreationError{Err: err}).Error())
	}

	// Sync registries in background (intentionally detached from request context)
	if created.AccessToken != nil && *created.AccessToken != "" {
		go func(envID string, envName string) { //nolint:contextcheck // intentional background context for async task
			bgCtx := context.Background()
			if err := h.environmentService.SyncRegistriesToEnvironment(bgCtx, envID); err != nil {
				slog.WarnContext(bgCtx, "Failed to sync registries to new environment",
					"environmentID", envID, "environmentName", envName, "error", err.Error())
			}
		}(created.ID, created.Name)
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}

	return &CreateEnvironmentOutput{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetEnvironment returns an environment by ID.
func (h *EnvironmentHandler) GetEnvironment(ctx context.Context, input *GetEnvironmentInput) (*GetEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.EnvironmentNotFoundError{}).Error())
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](env)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}

	return &GetEnvironmentOutput{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateEnvironment updates an environment.
func (h *EnvironmentHandler) UpdateEnvironment(ctx context.Context, input *UpdateEnvironmentInput) (*UpdateEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	isLocalEnv := input.ID == localDockerEnvironmentID
	updates := h.buildUpdateMap(&input.Body, isLocalEnv)

	pairingSucceeded, err := h.handleEnvironmentPairing(ctx, input.ID, &input.Body, updates, isLocalEnv)
	if err != nil {
		return nil, err
	}

	updated, updateErr := h.environmentService.UpdateEnvironment(ctx, input.ID, updates)
	if updateErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentUpdateError{Err: updateErr}).Error())
	}

	h.triggerPostUpdateTasks(input.ID, updated, pairingSucceeded, &input.Body) //nolint:contextcheck // intentionally detached background tasks

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}

	return &UpdateEnvironmentOutput{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteEnvironment deletes an environment.
func (h *EnvironmentHandler) DeleteEnvironment(ctx context.Context, input *DeleteEnvironmentInput) (*DeleteEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == localDockerEnvironmentID {
		return nil, huma.Error400BadRequest((&common.LocalEnvironmentDeletionError{}).Error())
	}

	if err := h.environmentService.DeleteEnvironment(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentDeletionError{Err: err}).Error())
	}

	return &DeleteEnvironmentOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment deleted successfully",
			},
		},
	}, nil
}

// TestConnection tests connectivity to an environment.
func (h *EnvironmentHandler) TestConnection(ctx context.Context, input *TestConnectionInput) (*TestConnectionOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	var apiUrl *string
	if input.Body != nil {
		apiUrl = input.Body.ApiUrl
	}

	status, err := h.environmentService.TestConnection(ctx, input.ID, apiUrl)
	resp := environment.Test{Status: status}
	if err != nil {
		msg := err.Error()
		resp.Message = &msg
		return &TestConnectionOutput{
			Body: base.ApiResponse[environment.Test]{
				Success: false,
				Data:    resp,
			},
		}, err
	}

	return &TestConnectionOutput{
		Body: base.ApiResponse[environment.Test]{
			Success: true,
			Data:    resp,
		},
	}, nil
}

// UpdateHeartbeat updates the heartbeat for an environment.
func (h *EnvironmentHandler) UpdateHeartbeat(ctx context.Context, input *UpdateHeartbeatInput) (*UpdateHeartbeatOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError((&common.HeartbeatUpdateError{Err: err}).Error())
	}

	return &UpdateHeartbeatOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Heartbeat updated successfully",
			},
		},
	}, nil
}

// PairAgent generates or rotates the local agent pairing token.
func (h *EnvironmentHandler) PairAgent(ctx context.Context, input *PairAgentInput) (*PairAgentOutput, error) {
	if h.environmentService == nil || h.settingsService == nil || h.cfg == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID != localDockerEnvironmentID {
		return nil, huma.Error404NotFound("Not found")
	}

	shouldRotate := input.Body != nil && input.Body.Rotate != nil && *input.Body.Rotate
	if h.cfg.AgentToken == "" || shouldRotate {
		h.cfg.AgentToken = utils.GenerateRandomString(48)
	}

	if err := h.settingsService.SetStringSetting(ctx, "agentToken", h.cfg.AgentToken); err != nil {
		return nil, huma.Error500InternalServerError((&common.AgentTokenPersistenceError{Err: err}).Error())
	}

	return &PairAgentOutput{
		Body: base.ApiResponse[environment.AgentPairResponse]{
			Success: true,
			Data: environment.AgentPairResponse{
				Token: h.cfg.AgentToken,
			},
		},
	}, nil
}

// SyncRegistries syncs container registries to an environment.
func (h *EnvironmentHandler) SyncRegistries(ctx context.Context, input *SyncRegistriesInput) (*SyncRegistriesOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.environmentService.SyncRegistriesToEnvironment(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistrySyncError{Err: err}).Error())
	}

	return &SyncRegistriesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registries synced successfully",
			},
		},
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

func (h *EnvironmentHandler) buildUpdateMap(req *environment.Update, isLocalEnv bool) map[string]any {
	updates := map[string]any{}

	if !isLocalEnv {
		if req.ApiUrl != nil {
			updates["api_url"] = *req.ApiUrl
		}
		if req.Enabled != nil {
			updates["enabled"] = *req.Enabled
		}
	}

	if req.Name != nil {
		updates["name"] = *req.Name
	}

	return updates
}

func (h *EnvironmentHandler) handleEnvironmentPairing(ctx context.Context, environmentID string, req *environment.Update, updates map[string]any, isLocalEnv bool) (bool, error) {
	pairingSucceeded := false

	if isLocalEnv {
		return pairingSucceeded, nil
	}

	if req.AccessToken == nil && req.BootstrapToken != nil && *req.BootstrapToken != "" {
		current, err := h.environmentService.GetEnvironmentByID(ctx, environmentID)
		if err != nil || current == nil {
			return false, huma.Error404NotFound("Environment not found")
		}

		apiUrl := current.ApiUrl
		if req.ApiUrl != nil && *req.ApiUrl != "" {
			apiUrl = *req.ApiUrl
		}

		if _, err := h.environmentService.PairAndPersistAgentToken(ctx, environmentID, apiUrl, *req.BootstrapToken); err != nil {
			return false, huma.Error502BadGateway("Agent pairing failed: " + err.Error())
		}
		pairingSucceeded = true
	} else if req.AccessToken != nil {
		updates["access_token"] = *req.AccessToken
	}

	return pairingSucceeded, nil
}

func (h *EnvironmentHandler) triggerPostUpdateTasks(environmentID string, updated *models.Environment, pairingSucceeded bool, req *environment.Update) { //nolint:contextcheck // intentionally spawns background tasks
	if updated.Enabled {
		go func(envID string, envName string) {
			ctx := context.Background()
			status, err := h.environmentService.TestConnection(ctx, envID, nil)
			if err != nil {
				slog.WarnContext(ctx, "Failed to test connection after environment update",
					"environment_id", envID, "environment_name", envName, "status", status, "error", err)
			}
		}(environmentID, updated.Name)
	}

	if pairingSucceeded || (req.AccessToken != nil && *req.AccessToken != "") {
		go func(envID string, envName string) {
			ctx := context.Background()
			if err := h.environmentService.SyncRegistriesToEnvironment(ctx, envID); err != nil {
				slog.WarnContext(ctx, "Failed to sync registries after environment update",
					"environmentID", envID, "environmentName", envName, "error", err.Error())
			}
		}(environmentID, updated.Name)
	}
}
