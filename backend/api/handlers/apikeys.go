package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/apikey"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

// apiKeyHandler provides Huma-based API key management endpoints.
type apiKeyHandler struct {
	apiKeyService *services.ApiKeyService
}

// --- Huma Input/Output Wrappers ---

// apiKeyPaginatedResponse is the paginated response for API keys.
type apiKeyPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []apikey.ApiKey         `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type listApiKeysInput struct {
	Search string `query:"search" doc:"Search query for filtering by name or description"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type listApiKeysOutput struct {
	Body apiKeyPaginatedResponse
}

type createApiKeyInput struct {
	Body apikey.CreateApiKey
}

type createMyApiKeyInput struct {
	Body apikey.CreateUserApiKey
}

type createApiKeyOutput struct {
	Body base.ApiResponse[apikey.ApiKeyCreatedDto]
}

type getApiKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

type getApiKeyOutput struct {
	Body base.ApiResponse[apikey.ApiKey]
}

type updateApiKeyInput struct {
	ID   string `path:"id" doc:"API key ID"`
	Body apikey.UpdateApiKey
}

type updateApiKeyOutput struct {
	Body base.ApiResponse[apikey.ApiKey]
}

type deleteApiKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

type deleteApiKeyOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type listMyApiKeysOutput struct {
	Body base.ApiResponse[[]apikey.ApiKey]
}

// RegisterApiKeys registers API key management routes using Huma.
func RegisterApiKeys(api huma.API, apiKeyService *services.ApiKeyService) {
	h := &apiKeyHandler{
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
	}, h.listApiKeysInternal)

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
	}, h.createApiKeyInternal)

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
	}, h.getApiKeyInternal)

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
	}, h.updateApiKeyInternal)

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
	}, h.deleteApiKeyInternal)

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
	}, h.listMyApiKeysInternal)

	// Personal keys inherit the owner's permissions, so creating or deleting
	// them is session-only (BearerAuth, no ApiKeyAuth): a stolen API key must
	// not be able to mint or remove persistence credentials.
	huma.Register(api, huma.Operation{
		OperationID: "create-my-api-key",
		Method:      http.MethodPost,
		Path:        "/auth/me/api-keys",
		Summary:     "Create my API key",
		Description: "Create a new personal API key owned by the current user. Personal keys inherit the owner's role permissions.",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
		},
	}, h.createMyApiKeyInternal)

	huma.Register(api, huma.Operation{
		OperationID: "delete-my-api-key",
		Method:      http.MethodDelete,
		Path:        "/auth/me/api-keys/{id}",
		Summary:     "Delete my API key",
		Description: "Delete one of the current user's own API keys",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
		},
	}, h.deleteMyApiKeyInternal)
}

// ListApiKeys returns a paginated list of API keys.
func (h *apiKeyHandler) listApiKeysInternal(ctx context.Context, input *listApiKeysInput) (*listApiKeysOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	apiKeys, paginationResp, err := h.apiKeyService.ListApiKeys(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ApiKeyListError{Err: err}).Error())
	}

	return &listApiKeysOutput{
		Body: apiKeyPaginatedResponse{
			Success:    true,
			Data:       apiKeys,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// CreateApiKey creates a new scoped API key. Requested grants are capped by
// the calling credential's effective permissions.
func (h *apiKeyHandler) createApiKeyInternal(ctx context.Context, input *createApiKeyInput) (*createApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	callerPerms, _ := humamw.PermissionsFromContext(ctx)
	apiKey, err := h.apiKeyService.CreateApiKey(ctx, user.ID, callerPerms, input.Body)
	if err != nil {
		if errors.Is(err, services.ErrApiKeyPermissionEscalation) {
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyCreationError{Err: err}).Error())
	}

	return &createApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKeyCreatedDto]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// GetApiKey returns details of a specific API key.
func (h *apiKeyHandler) getApiKeyInternal(ctx context.Context, input *getApiKeyInput) (*getApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	apiKey, err := h.apiKeyService.GetApiKey(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ApiKeyNotFoundError{}).Error())
	}

	return &getApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKey]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// UpdateApiKey updates an existing API key.
func (h *apiKeyHandler) updateApiKeyInternal(ctx context.Context, input *updateApiKeyInput) (*updateApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if _, err := requireUserInternal(ctx); err != nil {
		return nil, err
	}

	callerPerms, _ := humamw.PermissionsFromContext(ctx)
	apiKey, err := h.apiKeyService.UpdateApiKey(ctx, callerPerms, input.ID, input.Body)
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
		if errors.Is(err, services.ErrApiKeyPersonalNoGrants) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.ApiKeyUpdateError{Err: err}).Error())
	}

	return &updateApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKey]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// DeleteApiKey deletes an API key.
func (h *apiKeyHandler) deleteApiKeyInternal(ctx context.Context, input *deleteApiKeyInput) (*deleteApiKeyOutput, error) {
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

	return &deleteApiKeyOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "API key deleted successfully",
			},
		},
	}, nil
}

// ListMyApiKeys lists API keys owned by the current user (self-service).
func (h *apiKeyHandler) listMyApiKeysInternal(ctx context.Context, _ *struct{}) (*listMyApiKeysOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := h.apiKeyService.ListApiKeysByUser(ctx, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ApiKeyListError{Err: err}).Error())
	}

	return &listMyApiKeysOutput{
		Body: base.ApiResponse[[]apikey.ApiKey]{
			Success: true,
			Data:    keys,
		},
	}, nil
}

// CreateMyApiKey creates a new personal API key owned by the current user
// (self-service). Personal keys inherit the owner's role permissions, and may
// only be minted from an interactive session — never by another API key.
func (h *apiKeyHandler) createMyApiKeyInternal(ctx context.Context, input *createMyApiKeyInput) (*createApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// Defense in depth alongside the BearerAuth-only Security requirement:
	// only session auth sets a session ID, so API-key and sudo callers stop here.
	if _, ok := humamw.GetCurrentSessionIDFromContext(ctx); !ok {
		return nil, huma.Error403Forbidden("personal API keys can only be managed from an interactive session")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	apiKey, err := h.apiKeyService.CreatePersonalApiKey(ctx, user.ID, input.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ApiKeyCreationError{Err: err}).Error())
	}

	return &createApiKeyOutput{
		Body: base.ApiResponse[apikey.ApiKeyCreatedDto]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// DeleteMyApiKey deletes one of the current user's API keys, validating
// ownership before removal so the endpoint can't be used to delete other
// users' keys.
func (h *apiKeyHandler) deleteMyApiKeyInternal(ctx context.Context, input *deleteApiKeyInput) (*deleteApiKeyOutput, error) {
	if h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// Defense in depth alongside the BearerAuth-only Security requirement:
	// only session auth sets a session ID, so API-key and sudo callers stop here.
	if _, ok := humamw.GetCurrentSessionIDFromContext(ctx); !ok {
		return nil, huma.Error403Forbidden("personal API keys can only be managed from an interactive session")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
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

	return &deleteApiKeyOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "API key deleted successfully",
			},
		},
	}, nil
}
