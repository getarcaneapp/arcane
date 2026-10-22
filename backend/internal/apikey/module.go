// Package apikey owns API-key persistence, permission grants, validation, and
// the API-key HTTP surface.
package apikey

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *ApiKeyService
}

func New(service *ApiKeyService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *ApiKeyService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterApiKeys(api, nil)
		return
	}
	RegisterApiKeys(api, m.service)
}

// RegisterApiKeys registers API key management routes using Huma.
func RegisterApiKeys(api huma.API, apiKeyService *ApiKeyService) {
	h := &ApiKeyHandler{
		apiKeyService: apiKeyService,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-api-keys",
		Method:      http.MethodGet,
		Path:        "/apikeys",
		Summary:     "List API keys",
		Description: "Get a paginated list of API keys",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermApiKeysList, h.ListApiKeys)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-api-key",
		Method:      http.MethodPost,
		Path:        "/apikeys",
		Summary:     "Create an API key",
		Description: "Create a new API key for programmatic access",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermApiKeysCreate, h.CreateApiKey)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-api-key",
		Method:      http.MethodGet,
		Path:        "/apikeys/{id}",
		Summary:     "Get an API key",
		Description: "Get details of a specific API key by ID",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermApiKeysRead, h.GetApiKey)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-api-key",
		Method:      http.MethodPatch,
		Path:        "/apikeys/{id}",
		Summary:     "Update an API key",
		Description: "Update an existing API key's details",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermApiKeysUpdate, h.UpdateApiKey)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-api-key",
		Method:      http.MethodDelete,
		Path:        "/apikeys/{id}",
		Summary:     "Delete an API key",
		Description: "Delete an API key by ID",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermApiKeysDelete, h.DeleteApiKey)

	// Self-service endpoints — no admin permission required, scoped to the
	// caller's own keys via current-user context.
	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "list-my-api-keys",
		Method:      http.MethodGet,
		Path:        "/auth/me/apikeys",
		Summary:     "List my API keys",
		Description: "List API keys owned by the current user",
		Tags:        []string{"API Keys"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.ListMyApiKeys)

	// Personal keys inherit the owner's permissions, so creating or deleting
	// them is session-only (BearerAuth, no ApiKeyAuth): a stolen API key must
	// not be able to mint or remove persistence credentials.
	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "create-my-api-key",
		Method:      http.MethodPost,
		Path:        "/auth/me/apikeys",
		Summary:     "Create my API key",
		Description: "Create a new personal API key owned by the current user. Personal keys inherit the owner's role permissions.",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
		},
	}, h.CreateMyApiKey)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "delete-my-api-key",
		Method:      http.MethodDelete,
		Path:        "/auth/me/apikeys/{id}",
		Summary:     "Delete my API key",
		Description: "Delete one of the current user's own API keys",
		Tags:        []string{"API Keys"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
		},
	}, h.DeleteMyApiKey)
}
