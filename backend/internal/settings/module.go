// Package settings owns persisted application settings and their HTTP surface.
package settings

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	// Module joins the settings service with its route dependencies.
	Module struct {
		service         *SettingsService
		search          *SettingsSearchService
		proxyRemoteJSON handlerutil.RemoteJSONProxy
		config          *config.Config
	}
)

// New builds the settings domain around its initialized service.
func New(service *SettingsService, search *SettingsSearchService, proxyRemoteJSON handlerutil.RemoteJSONProxy, cfg *config.Config) *Module {
	return &Module{service: service, search: search, proxyRemoteJSON: proxyRemoteJSON, config: cfg}
}

// Service exposes settings operations to collaborating domains.
func (m *Module) Service() *SettingsService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts settings endpoints for runtime and schema discovery.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterSettings(api, nil, nil, nil, nil)
		return
	}
	RegisterSettings(api, m.service, m.search, m.proxyRemoteJSON, m.config)
}

// RegisterSettings registers settings management routes using Huma.
func RegisterSettings(api huma.API, settingsService *SettingsService, settingsSearchService *SettingsSearchService, proxyRemoteJSON handlerutil.RemoteJSONProxy, cfg *config.Config) {
	h := &SettingsHandler{
		settingsService:       settingsService,
		settingsSearchService: settingsSearchService,
		proxyRemoteJSON:       proxyRemoteJSON,
		cfg:                   cfg,
	}

	// Environment-scoped settings endpoints
	huma.Register(api, huma.Operation{
		OperationID: "get-public-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/settings/public",
		Summary:     "Get public settings",
		Description: "Get the settings the UI needs for an environment. Anonymous callers receive public settings; signed-in callers of any role also receive the non-admin settings",
		Tags:        []string{"Settings"},
		Security:    []map[string][]string{},
	}, h.GetPublicSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-environment-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/settings",
		Summary:     "Get environment settings",
		Description: "Get the environment-scoped settings for an environment",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentSettingsRead, h.GetEnvironmentSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-environment-settings",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/settings",
		Summary:     "Update environment settings",
		Description: "Update the environment-scoped settings for an environment. App-wide and authentication settings are rejected",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentSettingsUpdate, h.UpdateEnvironmentSettings)

	// Top-level settings endpoints (not environment-scoped)
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "Get app-wide settings",
		Description: "Get the app-wide settings owned by the manager, excluding authentication and OIDC",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsRead, h.GetManagerSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-settings",
		Method:      http.MethodPatch,
		Path:        "/settings",
		Summary:     "Update app-wide settings",
		Description: "Update the app-wide settings owned by the manager. Environment and authentication settings are rejected",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsUpdate, h.UpdateManagerSettings)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "get-authentication-settings",
		Method:      http.MethodGet,
		Path:        "/settings/authentication",
		Summary:     "Get authentication settings",
		Description: "Get the local authentication, session, and OIDC settings. Requires a global admin",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.GetAuthenticationSettings)

	huma.Register(api, middleware.RequireGlobalAdmin(api, huma.Operation{
		OperationID: "update-authentication-settings",
		Method:      http.MethodPatch,
		Path:        "/settings/authentication",
		Summary:     "Update authentication settings",
		Description: "Update the local authentication, session, and OIDC settings. Requires a global admin",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}), h.UpdateAuthenticationSettings)

	huma.Register(api, huma.Operation{
		OperationID: "search-settings",
		Method:      http.MethodPost,
		Path:        "/settings/search",
		Summary:     "Search settings",
		Description: "Search settings categories and individual settings by query",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.Search)

	huma.Register(api, huma.Operation{
		OperationID: "get-settings-categories",
		Method:      http.MethodGet,
		Path:        "/settings/categories",
		Summary:     "Get settings categories",
		Description: "Get all available settings categories with metadata",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetCategories)
}
