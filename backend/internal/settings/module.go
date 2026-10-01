// Package settings owns persisted application settings and their HTTP surface.
package settings

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Module joins the settings service with its route dependencies.
type Module struct {
	service         *SettingsService
	search          *SettingsSearchService
	proxyRemoteJSON handlerutil.RemoteJSONProxy
	config          *config.Config
}

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
