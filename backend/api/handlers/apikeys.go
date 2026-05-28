package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/apikey"
	"github.com/getarcaneapp/arcane/types/base"
)

// ApiKeyHandler provides Huma-based API key management endpoints.
type ApiKeyHandler struct {
	apiKeyService *services.ApiKeyService
}

// --- Huma Input/Output Wrappers ---

// ApiKeyPaginatedResponse is the paginated response for API keys.
type ApiKeyPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []apikey.ApiKey         `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListApiKeysInput struct {
	Search string `query:"search" doc:"Search query for filtering by name or description"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListApiKeysOutput struct {
	Body ApiKeyPaginatedResponse
}

type CreateApiKeyInput struct {
	Body apikey.CreateApiKey
}

type CreateApiKeyOutput struct {
	Body base.ApiResponse[apikey.ApiKeyCreatedDto]
}

type GetApiKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

type GetApiKeyOutput struct {
	Body base.ApiResponse[apikey.ApiKey]
}

type UpdateApiKeyInput struct {
	ID   string `path:"id" doc:"API key ID"`
	Body apikey.UpdateApiKey
}

type UpdateApiKeyOutput struct {
	Body base.ApiResponse[apikey.ApiKey]
}

type DeleteApiKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

type DeleteApiKeyOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type ListMyApiKeysOutput struct {
	Body base.ApiResponse[[]apikey.ApiKey]
}

// RegisterApiKeys registers API key management routes using Huma.
func RegisterApiKeys(api huma.API, apiKeyService *services.ApiKeyService) {
	h := &ApiKeyHandler{
		apiKeyService: apiKeyService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-api-keys",
		Method:      http.MethodGet,
		Path:        "/api-keys",
		Summary:     "List API keys",
		Description: "Get a paginated list of API keys",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermApiKeysList),
	}, h.ListApiKeys)

	huma.Register(api, huma.Operation{
		OperationID: "create-api-key",
		Method:      http.MethodPost,
		Path:        "/api-keys",
		Summary:     "Create an API key",
		Description: "Create a new API key for programmatic access",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermApiKeysCreate),
	}, h.CreateApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "get-api-key",
		Method:      http.MethodGet,
		Path:        "/api-keys/{id}",
		Summary:     "Get an API key",
		Description: "Get details of a specific API key by ID",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermApiKeysRead),
	}, h.GetApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "update-api-key",
		Method:      http.MethodPut,
		Path:        "/api-keys/{id}",
		Summary:     "Update an API key",
		Description: "Update an existing API key's details",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermApiKeysUpdate),
	}, h.UpdateApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "delete-api-key",
		Method:      http.MethodDelete,
		Path:        "/api-keys/{id}",
		Summary:     "Delete an API key",
		Description: "Delete an API key by ID",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermApiKeysDelete),
	}, h.DeleteApiKey)

	// Self-service endpoints — no admin permission required, scoped to the
	// caller's own keys via current-user context.
	huma.Register(api, huma.Operation{
		OperationID: "list-my-api-keys",
		Method:      http.MethodGet,
		Path:        "/auth/me/api-keys",
		Summary:     "List my API keys",
		Description: "List API keys owned by the current user",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListMyApiKeys)

	huma.Register(api, huma.Operation{
		OperationID: "create-my-api-key",
		Method:      http.MethodPost,
		Path:        "/auth/me/api-keys",
		Summary:     "Create my API key",
		Description: "Create a new API key owned by the current user",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateMyApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "delete-my-api-key",
		Method:      http.MethodDelete,
		Path:        "/auth/me/api-keys/{id}",
		Summary:     "Delete my API key",
		Description: "Delete one of the current user's own API keys",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteMyApiKey)
}

// ListApiKeys returns a paginated list of API keys.
func (h *ApiKeyHandler) ListApiKeys(ctx context.Context, input *ListApiKeysInput) (*ListApiKeysOutput, error) {
	if h.apiKeyService == nil {
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

	apiKeys, paginationResp, err := h.apiKeyService.ListApiKeys(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ApiKeyListError{Err: err}).Error())
	}

	return &ListApiKeysOutput{
		Body: ApiKeyPaginatedResponse{
			Success: true,
			Data:    apiKeys,
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

// CreateApiKey creates a new API key.
func (h *ApiKeyHandler) CreateApiKey(ctx context.Context, input *CreateApiKeyInput) (*CreateApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	apiKey, err := h.apiKeyService.CreateApiKey(ctx, user.ID, input.Body)
	if err != nil {
		if errors.Is(err, services.ErrApiKeyPermissionEscalation) {
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyCreationError{Err: err}).Error())
	}

	return &CreateApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKeyCreatedDto]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// GetApiKey returns details of a specific API key.
func (h *ApiKeyHandler) GetApiKey(ctx context.Context, input *GetApiKeyInput) (*GetApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	apiKey, err := h.apiKeyService.GetApiKey(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
	}

	return &GetApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKey]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// UpdateApiKey updates an existing API key.
func (h *ApiKeyHandler) UpdateApiKey(ctx context.Context, input *UpdateApiKeyInput) (*UpdateApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	apiKey, err := h.apiKeyService.UpdateApiKey(ctx, user.ID, input.ID, input.Body)
	if err != nil {
		if errors.Is(err, services.ErrApiKeyNotFound) {
			return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
		}
		if errors.Is(err, services.ErrApiKeyProtected) {
			return nil, huma.Error403Forbidden("static API keys cannot be updated")
		}
		if errors.Is(err, services.ErrApiKeyPermissionEscalation) {
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyUpdateError{Err: err}).Error())
	}

	return &UpdateApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKey]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// DeleteApiKey deletes an API key.
func (h *ApiKeyHandler) DeleteApiKey(ctx context.Context, input *DeleteApiKeyInput) (*DeleteApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.apiKeyService.DeleteApiKey(ctx, input.ID); err != nil {
		if errors.Is(err, services.ErrApiKeyNotFound) {
			return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
		}
		if errors.Is(err, services.ErrApiKeyProtected) {
			return nil, huma.Error403Forbidden("static API keys cannot be deleted")
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyDeletionError{Err: err}).Error())
	}

	return &DeleteApiKeyOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "API key deleted successfully",
			},
		},
	}, nil
}

// ListMyApiKeys lists API keys owned by the current user (self-service).
func (h *ApiKeyHandler) ListMyApiKeys(ctx context.Context, input *struct{}) (*ListMyApiKeysOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	keys, err := h.apiKeyService.ListApiKeysByUser(ctx, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ApiKeyListError{Err: err}).Error())
	}

	return &ListMyApiKeysOutput{
		Body: base.ApiResponse[[]apikey.ApiKey]{
			Success: true,
			Data:    keys,
		},
	}, nil
}

// CreateMyApiKey creates a new API key owned by the current user (self-service).
func (h *ApiKeyHandler) CreateMyApiKey(ctx context.Context, input *CreateApiKeyInput) (*CreateApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	apiKey, err := h.apiKeyService.CreateApiKey(ctx, user.ID, input.Body)
	if err != nil {
		if errors.Is(err, services.ErrApiKeyPermissionEscalation) {
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyCreationError{Err: err}).Error())
	}

	return &CreateApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKeyCreatedDto]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// DeleteMyApiKey deletes one of the current user's API keys, validating
// ownership before removal so the endpoint can't be used to delete other
// users' keys.
func (h *ApiKeyHandler) DeleteMyApiKey(ctx context.Context, input *DeleteApiKeyInput) (*DeleteApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	existing, err := h.apiKeyService.GetApiKey(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
	}
	if existing.UserID == nil || *existing.UserID != user.ID {
		return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
	}

	if err := h.apiKeyService.DeleteApiKey(ctx, input.ID); err != nil {
		if errors.Is(err, services.ErrApiKeyNotFound) {
			return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
		}
		if errors.Is(err, services.ErrApiKeyProtected) {
			return nil, huma.Error403Forbidden("this API key cannot be deleted")
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyDeletionError{Err: err}).Error())
	}

	return &DeleteApiKeyOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "API key deleted successfully",
			},
		},
	}, nil
}
