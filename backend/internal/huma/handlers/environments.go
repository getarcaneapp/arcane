package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/backend/internal/utils/stringutils"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/environment"
	"github.com/getarcaneapp/arcane/types/version"
)

const localDockerEnvironmentID = "0"

// EnvironmentHandler handles environment management endpoints.
type EnvironmentHandler struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	apiKeyService      *services.ApiKeyService
	eventService       *services.EventService
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
	Search string `query:"search" doc:"Search query for filtering by name or API URL"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListEnvironmentsOutput struct {
	Body EnvironmentPaginatedResponse
}

type CreateEnvironmentInput struct {
	Body environment.Create
}

type EnvironmentWithApiKey struct {
	environment.Environment
	ApiKey *string `json:"apiKey,omitempty" doc:"API key for pairing (only shown once during creation)"`
}

type CreateEnvironmentOutput struct {
	Body base.ApiResponse[EnvironmentWithApiKey]
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

type SyncEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type SyncEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PairEnvironmentInput struct {
	XAPIKey string `header:"X-API-Key" doc:"API key for environment pairing"`
}

type PairEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DeploymentSnippet struct {
	DockerRun     string `json:"dockerRun" doc:"Docker run command snippet"`
	DockerCompose string `json:"dockerCompose" doc:"Docker compose YAML snippet"`
}

type GetDeploymentSnippetsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetDeploymentSnippetsOutput struct {
	Body base.ApiResponse[DeploymentSnippet]
}

type GetEnvironmentVersionInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentVersionOutput struct {
	Body base.ApiResponse[version.Info]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterEnvironments registers all environment management endpoints.
func RegisterEnvironments(api huma.API, environmentService *services.EnvironmentService, settingsService *services.SettingsService, apiKeyService *services.ApiKeyService, eventService *services.EventService, cfg *config.Config) {
	h := &EnvironmentHandler{
		environmentService: environmentService,
		settingsService:    settingsService,
		apiKeyService:      apiKeyService,
		eventService:       eventService,
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
		Description: "Delete a Arcane environment",
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
		Description: "Test connectivity to a Arcane environment",
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
		OperationID: "syncEnvironment",
		Method:      "POST",
		Path:        "/environments/{id}/sync",
		Summary:     "Sync environment",
		Description: "Sync container registries and git repositories to a remote environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.SyncEnvironment)

	huma.Register(api, huma.Operation{
		OperationID:  "pairEnvironment",
		Method:       "POST",
		Path:         "/environments/pair",
		Summary:      "Pair agent with manager",
		Description:  "Agent sends API key to complete environment pairing",
		Tags:         []string{"Environments"},
		MaxBodyBytes: 1024,
	}, h.PairEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "getDeploymentSnippets",
		Method:      "GET",
		Path:        "/environments/{id}/deployment",
		Summary:     "Get deployment snippets",
		Description: "Get Docker run and compose snippets for environment deployment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetDeploymentSnippets)

	huma.Register(api, huma.Operation{
		OperationID: "getEnvironmentVersion",
		Method:      "GET",
		Path:        "/environments/{id}/version",
		Summary:     "Get environment version",
		Description: "Get the version of a remote environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetEnvironmentVersion)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListEnvironments returns a paginated list of environments.
func (h *EnvironmentHandler) ListEnvironments(ctx context.Context, input *ListEnvironmentsInput) (*ListEnvironmentsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: input.Search,
		},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
	}

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
	if h.environmentService == nil || h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
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

	// Determine pairing method
	useApiKey := input.Body.UseApiKey != nil && *input.Body.UseApiKey

	if useApiKey {
		return h.createEnvironmentWithApiKey(ctx, env, user)
	}

	return h.createEnvironmentLegacy(ctx, env, user, input.Body)
}

func (h *EnvironmentHandler) createEnvironmentWithApiKey(ctx context.Context, env *models.Environment, user *models.User) (*CreateEnvironmentOutput, error) {
	// New API key-based pairing flow
	env.Status = string(models.EnvironmentStatusPending)

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentCreationError{Err: err}).Error())
	}

	// Generate API key for environment
	apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, created.ID, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create environment API key", "environmentID", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to create environment API key")
	}

	// Store the API key in AccessToken field (encrypted) for manager-to-agent auth
	encryptedKey := apiKeyDto.Key // Store the full key

	// Link API key to environment and store encrypted key for manager use
	updates := map[string]interface{}{
		"api_key_id":   apiKeyDto.ID,
		"access_token": encryptedKey,
	}
	created, err = h.environmentService.UpdateEnvironment(ctx, created.ID, updates, &user.ID, &user.Username)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to link API key to environment", "environmentID", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to link API key")
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}

	return &CreateEnvironmentOutput{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data: EnvironmentWithApiKey{
				Environment: out,
				ApiKey:      &apiKeyDto.Key,
			},
		},
	}, nil
}

func (h *EnvironmentHandler) createEnvironmentLegacy(ctx context.Context, env *models.Environment, user *models.User, body environment.Create) (*CreateEnvironmentOutput, error) {
	// Legacy pairing flows
	if (body.AccessToken == nil || *body.AccessToken == "") && body.BootstrapToken != nil && *body.BootstrapToken != "" {
		token, err := h.environmentService.PairAgentWithBootstrap(ctx, body.ApiUrl, *body.BootstrapToken)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to pair with agent", "apiUrl", body.ApiUrl, "error", err.Error())
			return nil, huma.Error502BadGateway((&common.AgentPairingError{Err: err}).Error())
		}
		env.AccessToken = &token
	} else if body.AccessToken != nil && *body.AccessToken != "" {
		env.AccessToken = body.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentCreationError{Err: err}).Error())
	}

	// Sync registries and git repositories in background (intentionally detached from request context)
	if created.AccessToken != nil && *created.AccessToken != "" {
		go func(envID string, envName string) { //nolint:contextcheck // intentional background context for async task
			bgCtx := context.Background()
			if err := h.environmentService.SyncRegistriesToEnvironment(bgCtx, envID); err != nil {
				slog.WarnContext(bgCtx, "Failed to sync registries to new environment",
					"environmentID", envID, "environmentName", envName, "error", err.Error())
			}
		}(created.ID, created.Name)
		go func(envID string, envName string) { //nolint:contextcheck // intentional background context for async task
			bgCtx := context.Background()
			if err := h.environmentService.SyncRepositoriesToEnvironment(bgCtx, envID); err != nil {
				slog.WarnContext(bgCtx, "Failed to sync git repositories to new environment",
					"environmentID", envID, "environmentName", envName, "error", err.Error())
			}
		}(created.ID, created.Name)
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}

	return &CreateEnvironmentOutput{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data: EnvironmentWithApiKey{
				Environment: out,
			},
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

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	isLocalEnv := input.ID == localDockerEnvironmentID
	updates := h.buildUpdateMap(&input.Body, isLocalEnv)

	pairingSucceeded, err := h.handleEnvironmentPairing(ctx, input.ID, &input.Body, updates, isLocalEnv)
	if err != nil {
		return nil, err
	}

	user, _ := humamw.GetCurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = &user.ID
		username = &user.Username
	}
	updated, updateErr := h.environmentService.UpdateEnvironment(ctx, input.ID, updates, userID, username)
	if updateErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentUpdateError{Err: updateErr}).Error())
	}

	h.triggerPostUpdateTasks(input.ID, updated, pairingSucceeded, &input.Body) //nolint:contextcheck // intentionally detached background tasks

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}

	// If regenerating API key, return the new key
	var newApiKey *string
	if input.Body.RegenerateApiKey != nil && *input.Body.RegenerateApiKey {
		user, exists := humamw.GetCurrentUserFromContext(ctx)
		if !exists {
			return nil, huma.Error401Unauthorized("Unauthorized")
		}

		// Delete existing API key if any
		if updated.ApiKeyID != nil {
			_ = h.apiKeyService.DeleteApiKey(ctx, *updated.ApiKeyID)
		}

		// Generate new API key
		apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, input.ID, user.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create new environment API key", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Use service method to update environment and create event
		encryptedKey := apiKeyDto.Key
		err = h.environmentService.RegenerateEnvironmentApiKey(ctx, input.ID, apiKeyDto.ID, encryptedKey, user.ID, user.Username, updated.Name)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to regenerate API key", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Fetch updated environment
		updated, err = h.environmentService.GetEnvironmentByID(ctx, input.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to fetch updated environment", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to fetch updated environment")
		}

		// Re-map with updated environment data
		out, mapErr = mapper.MapOne[*models.Environment, environment.Environment](updated)
		if mapErr != nil {
			return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
		}

		newApiKey = &apiKeyDto.Key
	}

	// Set the API key on the response if regenerated
	out.ApiKey = newApiKey

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

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if input.ID == localDockerEnvironmentID {
		return nil, huma.Error400BadRequest((&common.LocalEnvironmentDeletionError{}).Error())
	}

	user, _ := humamw.GetCurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = &user.ID
		username = &user.Username
	}
	if err := h.environmentService.DeleteEnvironment(ctx, input.ID, userID, username); err != nil {
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

	if err := checkAdmin(ctx); err != nil {
		return nil, err
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

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if input.ID != localDockerEnvironmentID {
		return nil, huma.Error404NotFound("Not found")
	}

	shouldRotate := input.Body != nil && input.Body.Rotate != nil && *input.Body.Rotate
	if h.cfg.AgentToken == "" || shouldRotate {
		h.cfg.AgentToken = stringutils.GenerateRandomString(48)
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

// SyncEnvironment syncs container registries and git repositories to an environment.
func (h *EnvironmentHandler) SyncEnvironment(ctx context.Context, input *SyncEnvironmentInput) (*SyncEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	// Sync registries
	if err := h.environmentService.SyncRegistriesToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync registries", "environmentID", input.ID, "error", err.Error())
	}

	// Sync git repositories
	if err := h.environmentService.SyncRepositoriesToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync git repositories", "environmentID", input.ID, "error", err.Error())
	}

	return &SyncEnvironmentOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment synced successfully",
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
		go func(envID string, envName string) {
			ctx := context.Background()
			if err := h.environmentService.SyncRepositoriesToEnvironment(ctx, envID); err != nil {
				slog.WarnContext(ctx, "Failed to sync git repositories after environment update",
					"environmentID", envID, "environmentName", envName, "error", err.Error())
			}
		}(environmentID, updated.Name)
	}
}

// PairEnvironment handles agent pairing callback with API key.
func (h *EnvironmentHandler) PairEnvironment(ctx context.Context, input *PairEnvironmentInput) (*PairEnvironmentOutput, error) {
	if h.environmentService == nil || h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.XAPIKey == "" {
		return nil, huma.Error400BadRequest("X-API-Key header is required")
	}

	envID, err := h.apiKeyService.GetEnvironmentByApiKey(ctx, input.XAPIKey)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to validate API key for pairing", "error", err.Error())
		return nil, huma.Error401Unauthorized("Invalid API key")
	}

	if envID == nil {
		return nil, huma.Error400BadRequest("API key is not linked to an environment")
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, *envID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get environment", "environmentID", *envID, "error", err.Error())
		return nil, huma.Error404NotFound("Environment not found")
	}

	if env.Status != string(models.EnvironmentStatusPending) {
		return nil, huma.Error400BadRequest("Environment is not in pending status")
	}

	updates := map[string]interface{}{
		"status": string(models.EnvironmentStatusOnline),
	}
	_, err = h.environmentService.UpdateEnvironment(ctx, *envID, updates, nil, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update environment status", "environmentID", *envID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to complete pairing")
	}

	slog.InfoContext(ctx, "Environment pairing completed", "environmentID", *envID, "environmentName", env.Name)

	return &PairEnvironmentOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment pairing completed successfully",
			},
		},
	}, nil
}

// GetDeploymentSnippets returns deployment snippets for an environment.
func (h *EnvironmentHandler) GetDeploymentSnippets(ctx context.Context, input *GetDeploymentSnippetsInput) (*GetDeploymentSnippetsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	if env.ApiKeyID == nil {
		return nil, huma.Error400BadRequest("Environment does not have an API key configured")
	}

	// Generate snippets with placeholder for API key
	snippets, err := h.environmentService.GenerateDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), "<YOUR_API_KEY>")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate deployment snippets", "environmentID", input.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to generate deployment snippets")
	}

	return &GetDeploymentSnippetsOutput{
		Body: base.ApiResponse[DeploymentSnippet]{
			Success: true,
			Data: DeploymentSnippet{
				DockerRun:     snippets.DockerRun,
				DockerCompose: snippets.DockerCompose,
			},
		},
	}, nil
}

// GetEnvironmentVersion returns the version of a remote environment.
func (h *EnvironmentHandler) GetEnvironmentVersion(ctx context.Context, input *GetEnvironmentVersionInput) (*GetEnvironmentVersionOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	// Make HTTP request to the environment's /api/app-version endpoint
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	url := strings.TrimRight(env.ApiUrl, "/") + "/api/app-version"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create request")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, huma.Error500InternalServerError("Request failed: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, huma.Error500InternalServerError(fmt.Sprintf("Unexpected status code: %d", resp.StatusCode))
	}

	var versionInfo version.Info
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return nil, huma.Error500InternalServerError("Failed to decode version response")
	}

	// Update environment status to online since we successfully contacted it
	if updateErr := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); updateErr != nil {
		slog.WarnContext(ctx, "Failed to update environment heartbeat", "environment_id", input.ID, "error", updateErr)
		// Don't fail the request if heartbeat update fails
	}

	return &GetEnvironmentVersionOutput{
		Body: base.ApiResponse[version.Info]{
			Success: true,
			Data:    versionInfo,
		},
	}, nil
}
