package handlers

import (
	"cmp"
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
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

type ListTagsOutput struct {
	Body base.ApiResponse[[]string]
}

type ListFiltersOutput struct {
	Body base.ApiResponse[[]environment.FilterResponse]
}

type GetFilterInput struct {
	FilterID string `path:"filterId" doc:"Filter ID"`
}

type GetFilterOutput struct {
	Body base.ApiResponse[environment.FilterResponse]
}

type CreateFilterInput struct {
	Body environment.FilterCreate
}

type CreateFilterOutput struct {
	Body base.ApiResponse[environment.FilterResponse]
}

type UpdateFilterInput struct {
	FilterID string `path:"filterId" doc:"Filter ID"`
	Body     environment.FilterUpdate
}

type UpdateFilterOutput struct {
	Body base.ApiResponse[environment.FilterResponse]
}

type DeleteFilterInput struct {
	FilterID string `path:"filterId" doc:"Filter ID"`
}

type DeleteFilterOutput struct {
	Body base.ApiResponse[any]
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

	huma.Register(api, huma.Operation{
		OperationID: "listEnvironmentTags",
		Method:      "GET",
		Path:        "/environments/tags",
		Summary:     "List tags",
		Description: "List all unique tags used across environments",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListTags)

	huma.Register(api, huma.Operation{
		OperationID: "listEnvironmentFilters",
		Method:      "GET",
		Path:        "/environments/filters",
		Summary:     "List filters",
		Description: "List all filters for the current user",
		Tags:        []string{"Filters"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListFilters)

	huma.Register(api, huma.Operation{
		OperationID: "createEnvironmentFilter",
		Method:      "POST",
		Path:        "/environments/filters",
		Summary:     "Create a filter",
		Description: "Create a new filter",
		Tags:        []string{"Filters"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateFilter)

	huma.Register(api, huma.Operation{
		OperationID: "getEnvironmentFilter",
		Method:      "GET",
		Path:        "/environments/filters/{filterId}",
		Summary:     "Get a filter",
		Description: "Get a filter by ID",
		Tags:        []string{"Filters"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetFilter)

	huma.Register(api, huma.Operation{
		OperationID: "updateEnvironmentFilter",
		Method:      "PUT",
		Path:        "/environments/filters/{filterId}",
		Summary:     "Update a filter",
		Description: "Update an existing filter",
		Tags:        []string{"Filters"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateFilter)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEnvironmentFilter",
		Method:      "DELETE",
		Path:        "/environments/filters/{filterId}",
		Summary:     "Delete a filter",
		Description: "Delete an existing filter",
		Tags:        []string{"Filters"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteFilter)
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

// ListTags returns all unique tags used across environments.
func (h *EnvironmentHandler) ListTags(ctx context.Context, _ *struct{}) (*ListTagsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	tags, err := h.environmentService.ListTags(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &ListTagsOutput{
		Body: base.ApiResponse[[]string]{
			Success: true,
			Data:    tags,
		},
	}, nil
}

// ListFilters returns all filters for the current user.
func (h *EnvironmentHandler) ListFilters(ctx context.Context, _ *struct{}) (*ListFiltersOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userID, ok := humamw.GetUserIDFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	filters, err := h.environmentService.ListFilters(ctx, userID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.FilterListError{Err: err}).Error())
	}

	out, mapErr := mapper.MapSlice[models.EnvironmentFilter, environment.FilterResponse](filters)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.FilterMappingError{Err: mapErr}).Error())
	}

	return &ListFiltersOutput{
		Body: base.ApiResponse[[]environment.FilterResponse]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetFilter returns a filter by ID.
func (h *EnvironmentHandler) GetFilter(ctx context.Context, input *GetFilterInput) (*GetFilterOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userID, ok := humamw.GetUserIDFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	filter, err := h.environmentService.GetFilter(ctx, input.FilterID, userID)
	if err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			return nil, huma.Error404NotFound((&common.FilterNotFoundError{}).Error())
		}
		if errors.Is(err, services.ErrFilterForbidden) {
			return nil, huma.Error403Forbidden((&common.FilterForbiddenError{}).Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}

	out, mapErr := mapper.MapOne[*models.EnvironmentFilter, environment.FilterResponse](filter)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.FilterMappingError{Err: mapErr}).Error())
	}

	return &GetFilterOutput{
		Body: base.ApiResponse[environment.FilterResponse]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// CreateFilter creates a new filter.
func (h *EnvironmentHandler) CreateFilter(ctx context.Context, input *CreateFilterInput) (*CreateFilterOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userID, ok := humamw.GetUserIDFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	filter := &models.EnvironmentFilter{
		UserID:       userID,
		Name:         input.Body.Name,
		IsDefault:    input.Body.IsDefault,
		SearchQuery:  input.Body.SearchQuery,
		SelectedTags: input.Body.SelectedTags,
		ExcludedTags: input.Body.ExcludedTags,
		TagMode:      models.EnvironmentFilterTagMode(cmp.Or(input.Body.TagMode, string(models.TagModeAny))),
		StatusFilter: models.EnvironmentFilterStatusFilter(cmp.Or(input.Body.StatusFilter, string(models.StatusFilterAll))),
		GroupBy:      models.EnvironmentFilterGroupBy(cmp.Or(input.Body.GroupBy, string(models.GroupByNone))),
	}

	created, err := h.environmentService.CreateFilter(ctx, filter)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.FilterCreationError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.EnvironmentFilter, environment.FilterResponse](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.FilterMappingError{Err: mapErr}).Error())
	}

	return &CreateFilterOutput{
		Body: base.ApiResponse[environment.FilterResponse]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateFilter updates an existing filter.
func (h *EnvironmentHandler) UpdateFilter(ctx context.Context, input *UpdateFilterInput) (*UpdateFilterOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userID, ok := humamw.GetUserIDFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	updated, err := h.environmentService.UpdateFilter(ctx, input.FilterID, userID, &input.Body)
	if err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			return nil, huma.Error404NotFound((&common.FilterNotFoundError{}).Error())
		}
		if errors.Is(err, services.ErrFilterForbidden) {
			return nil, huma.Error403Forbidden((&common.FilterForbiddenError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.FilterUpdateError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.EnvironmentFilter, environment.FilterResponse](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.FilterMappingError{Err: mapErr}).Error())
	}

	return &UpdateFilterOutput{
		Body: base.ApiResponse[environment.FilterResponse]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteFilter deletes a filter.
func (h *EnvironmentHandler) DeleteFilter(ctx context.Context, input *DeleteFilterInput) (*DeleteFilterOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	userID, ok := humamw.GetUserIDFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.environmentService.DeleteFilter(ctx, input.FilterID, userID); err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			return nil, huma.Error404NotFound((&common.FilterNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.FilterDeleteError{Err: err}).Error())
	}

	return &DeleteFilterOutput{
		Body: base.ApiResponse[any]{
			Success: true,
			Data:    nil,
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

	if req.Tags != nil {
		updates["tags"] = req.Tags
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
