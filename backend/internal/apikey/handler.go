package apikey

import (
	"context"
	"net/http"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	apikeytypes "github.com/getarcaneapp/arcane/types/v2/apikey"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

// ApiKeyHandler provides Huma-based API key management endpoints.
type ApiKeyHandler struct {
	apiKeyService *ApiKeyService
}

// --- Huma Input/Output Wrappers ---

type ListApiKeysInput struct {
	Search string `query:"search" doc:"Search query for filtering by name or description"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type CreateApiKeyInput struct {
	Body apikeytypes.CreateApiKey
}

type CreateMyApiKeyInput struct {
	Body apikeytypes.CreateUserApiKey
}

type GetApiKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

type UpdateApiKeyInput struct {
	ID   string `path:"id" doc:"API key ID"`
	Body apikeytypes.UpdateApiKey
}

type DeleteApiKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

// RegisterApiKeys registers API key management routes using Huma.
func RegisterApiKeys(api huma.API, apiKeyService *ApiKeyService) {
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
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermApiKeysList),
	}, h.ListApiKeys)

	huma.Register(api, huma.Operation{
		OperationID: "create-api-key",
		Method:      http.MethodPost,
		Path:        "/api-keys",
		Summary:     "Create an API key",
		Description: "Create a new API key for programmatic access",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermApiKeysCreate),
	}, h.CreateApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "get-api-key",
		Method:      http.MethodGet,
		Path:        "/api-keys/{id}",
		Summary:     "Get an API key",
		Description: "Get details of a specific API key by ID",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermApiKeysRead),
	}, h.GetApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "update-api-key",
		Method:      http.MethodPut,
		Path:        "/api-keys/{id}",
		Summary:     "Update an API key",
		Description: "Update an existing API key's details",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermApiKeysUpdate),
	}, h.UpdateApiKey)

	huma.Register(api, huma.Operation{
		OperationID: "delete-api-key",
		Method:      http.MethodDelete,
		Path:        "/api-keys/{id}",
		Summary:     "Delete an API key",
		Description: "Delete an API key by ID",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermApiKeysDelete),
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
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.ListMyApiKeys)

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
		},
	}, h.DeleteMyApiKey)
}

// ListApiKeys returns a paginated list of API keys.
func (h *ApiKeyHandler) ListApiKeys(ctx context.Context, input *ListApiKeysInput) (*handlerutil.Page[apikeytypes.ApiKey], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	apiKeys, paginationResp, err := h.apiKeyService.ListApiKeys(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list API keys").Error())
	}

	return &handlerutil.Page[apikeytypes.ApiKey]{
		Body: base.Paginated[apikeytypes.ApiKey]{
			Success:    true,
			Data:       apiKeys,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// CreateApiKey creates a new scoped API key. Requested grants are capped by
// the calling credential's effective permissions.
func (h *ApiKeyHandler) CreateApiKey(ctx context.Context, input *CreateApiKeyInput) (*handlerutil.Out[apikeytypes.ApiKeyCreatedDto], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	callerPerms, _ := middleware.PermissionsFromContext(ctx)
	apiKey, err := h.apiKeyService.CreateApiKey(ctx, user.ID, callerPerms, input.Body)
	if err != nil {
		if errors.Is(err, ErrApiKeyPermissionEscalation) {
			return nil, huma.Error403Forbidden(err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to create API key")
	}

	return &handlerutil.Out[apikeytypes.ApiKeyCreatedDto]{
		Body: base.ApiResponse[apikeytypes.ApiKeyCreatedDto]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// GetApiKey returns details of a specific API key.
func (h *ApiKeyHandler) GetApiKey(ctx context.Context, input *GetApiKeyInput) (*handlerutil.Out[apikeytypes.ApiKey], error) {
	apiKey, err := h.apiKeyService.GetApiKey(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("API key not found")
	}

	return &handlerutil.Out[apikeytypes.ApiKey]{
		Body: base.ApiResponse[apikeytypes.ApiKey]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// UpdateApiKey updates an existing API key.
func (h *ApiKeyHandler) UpdateApiKey(ctx context.Context, input *UpdateApiKeyInput) (*handlerutil.Out[apikeytypes.ApiKey], error) {
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}

	callerPerms, _ := middleware.PermissionsFromContext(ctx)
	apiKey, err := h.apiKeyService.UpdateApiKey(ctx, callerPerms, input.ID, input.Body)
	if err != nil {
		if errors.Is(err, ErrApiKeyNotFound) {
			return nil, huma.Error404NotFound("API key not found")
		}
		if errors.Is(err, ErrApiKeyProtected) {
			return nil, huma.Error403Forbidden("static API keys cannot be updated")
		}
		if errors.Is(err, ErrApiKeyPermissionEscalation) {
			return nil, huma.Error403Forbidden(err.Error())
		}
		if errors.Is(err, ErrApiKeyPersonalNoGrants) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to update API key")
	}

	return &handlerutil.Out[apikeytypes.ApiKey]{
		Body: base.ApiResponse[apikeytypes.ApiKey]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// DeleteApiKey deletes an API key.
func (h *ApiKeyHandler) DeleteApiKey(ctx context.Context, input *DeleteApiKeyInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.apiKeyService.DeleteApiKey(ctx, input.ID); err != nil {
		if errors.Is(err, ErrApiKeyNotFound) {
			return nil, huma.Error404NotFound("API key not found")
		}
		if errors.Is(err, ErrApiKeyProtected) {
			return nil, huma.Error403Forbidden("static API keys cannot be deleted")
		}
		return nil, huma.Error500InternalServerError("Failed to delete API key")
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "API key deleted successfully",
			},
		},
	}, nil
}

// ListMyApiKeys lists API keys owned by the current user (self-service).
func (h *ApiKeyHandler) ListMyApiKeys(ctx context.Context, input *struct{}) (*handlerutil.Out[[]apikeytypes.ApiKey], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := h.apiKeyService.ListApiKeysByUser(ctx, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list API keys").Error())
	}

	return &handlerutil.Out[[]apikeytypes.ApiKey]{
		Body: base.ApiResponse[[]apikeytypes.ApiKey]{
			Success: true,
			Data:    keys,
		},
	}, nil
}

// CreateMyApiKey creates a new personal API key owned by the current user
// (self-service). Personal keys inherit the owner's role permissions, and may
// only be minted from an interactive session — never by another API key.
func (h *ApiKeyHandler) CreateMyApiKey(ctx context.Context, input *CreateMyApiKeyInput) (*handlerutil.Out[apikeytypes.ApiKeyCreatedDto], error) {
	// Defense in depth alongside the BearerAuth-only Security requirement:
	// only session auth sets a session ID, so API-key and sudo callers stop here.
	if _, ok := middleware.GetCurrentSessionIDFromContext(ctx); !ok {
		return nil, huma.Error403Forbidden("personal API keys can only be managed from an interactive session")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	apiKey, err := h.apiKeyService.CreatePersonalApiKey(ctx, user.ID, input.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create API key")
	}

	return &handlerutil.Out[apikeytypes.ApiKeyCreatedDto]{
		Body: base.ApiResponse[apikeytypes.ApiKeyCreatedDto]{
			Success: true,
			Data:    *apiKey,
		},
	}, nil
}

// DeleteMyApiKey deletes one of the current user's API keys, validating
// ownership before removal so the endpoint can't be used to delete other
// users' keys.
func (h *ApiKeyHandler) DeleteMyApiKey(ctx context.Context, input *DeleteApiKeyInput) (*handlerutil.Out[base.MessageResponse], error) {
	// Defense in depth alongside the BearerAuth-only Security requirement:
	// only session auth sets a session ID, so API-key and sudo callers stop here.
	if _, ok := middleware.GetCurrentSessionIDFromContext(ctx); !ok {
		return nil, huma.Error403Forbidden("personal API keys can only be managed from an interactive session")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := h.apiKeyService.GetApiKey(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("API key not found")
	}
	if existing.UserID == nil || *existing.UserID != user.ID {
		return nil, huma.Error404NotFound("API key not found")
	}

	if err := h.apiKeyService.DeleteApiKey(ctx, input.ID); err != nil {
		if errors.Is(err, ErrApiKeyNotFound) {
			return nil, huma.Error404NotFound("API key not found")
		}
		if errors.Is(err, ErrApiKeyProtected) {
			return nil, huma.Error403Forbidden("this API key cannot be deleted")
		}
		return nil, huma.Error500InternalServerError("Failed to delete API key")
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "API key deleted successfully",
			},
		},
	}, nil
}
