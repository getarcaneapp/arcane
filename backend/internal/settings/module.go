package settings

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

type (
	Module struct {
		service         *SettingsService
		search          *SettingsSearchService
		proxyRemoteJSON handlerutil.RemoteJSONProxy
		config          *config.Config
		mtls            *agent.MTLS
	}

	ModuleParams struct {
		Service         *SettingsService
		Search          *SettingsSearchService
		ProxyRemoteJSON handlerutil.RemoteJSONProxy
		Config          *config.Config
		MTLS            *agent.MTLS
	}
)

func New(p ModuleParams) *Module {
	return &Module{
		service:         p.Service,
		search:          p.Search,
		proxyRemoteJSON: p.ProxyRemoteJSON,
		config:          p.Config,
		mtls:            p.MTLS,
	}
}

func (m *Module) Service() *SettingsService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterSettings(api, &SettingsHandler{})
		return
	}
	RegisterSettings(api, &SettingsHandler{
		settingsService:       m.service,
		settingsSearchService: m.search,
		proxyRemoteJSON:       m.proxyRemoteJSON,
		cfg:                   m.config,
		mtls:                  m.mtls,
	})
}

func RegisterSettings(api huma.API, h *SettingsHandler) {
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
