// Package settings owns persisted application settings and their HTTP surface.
package settings

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Module joins the settings service with its route dependencies.
type Module struct {
	service         *SettingsService
	search          *SettingsSearchService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
	config          *config.Config
	audit           AuditRecorder
	roles           *role.RoleService
}

type AuditRecorder interface {
	Record(ctx context.Context, entry types.AuditEntry) error
}

// New builds the settings domain around its initialized service.
func New(service *SettingsService, search *SettingsSearchService, proxyRemoteJSON handlerutil.RemoteJSONProxy, cfg *config.Config, audit AuditRecorder, roles *role.RoleService) *Module {
	return &Module{
		service:         service,
		search:          search,
		proxyRemoteJSON: proxyRemoteJSON,
		config:          cfg,
		audit:           audit,
		roles:           roles,
	}
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
		RegisterSettings(api, nil, nil, nil, nil, nil, nil)
		return
	}
	RegisterSettings(api, m.service, m.search, m.proxyRemoteJSON, m.config, m.audit, m.roles)
}

// RegisterSettings registers settings management routes using Huma.
func RegisterSettings(
	api huma.API,
	settingsService *SettingsService,
	settingsSearchService *SettingsSearchService,
	proxyRemoteJSON handlerutil.RemoteJSONProxy,
	cfg *config.Config,
	audit AuditRecorder,
	roles *role.RoleService,
) {
	h := &SettingsHandler{
		settingsService:       settingsService,
		settingsSearchService: settingsSearchService,
		proxyRemoteJSON:       proxyRemoteJSON,
		cfg:                   cfg,
		audit:                 audit,
		roles:                 roles,
	}

	// Environment-scoped settings endpoints
	huma.Register(api, huma.Operation{
		OperationID: "get-public-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/settings/public",
		Summary:     "Get public settings",
		Description: "Get all public settings for an environment",
		Tags:        []string{"Settings"},
		Security:    []map[string][]string{},
	}, h.GetPublicSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/settings",
		Summary:     "Get settings",
		Description: "Get all settings for an environment",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsRead, h.GetSettings)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-settings",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/settings",
		Summary:     "Update settings",
		Description: "Update settings for an environment",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsWrite, h.UpdateSettings)

	// Top-level settings endpoints (not environment-scoped)
	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "search-settings",
		Method:      http.MethodPost,
		Path:        "/settings/search",
		Summary:     "Search settings",
		Description: "Search settings categories and individual settings by query",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.Search)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "get-settings-categories",
		Method:      http.MethodGet,
		Path:        "/settings/categories",
		Summary:     "Get settings categories",
		Description: "Get all available settings categories with metadata",
		Tags:        []string{"Settings"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetCategories)
}
