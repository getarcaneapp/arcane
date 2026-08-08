// Package settings owns persisted application settings and their HTTP surface.
package settings

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Dependencies are the handler-side collaborators for the settings domain.
type Dependencies struct {
	Search          *search.SettingsSearchService
	ProxyRemoteJSON handlerutil.RemoteJSONProxy
	Config          *config.Config
}

// Module joins the settings service with its route dependencies.
type Module struct {
	service *SettingsService
	deps    Dependencies
}

// New builds the settings domain around its initialized actor-backed service.
func New(service *SettingsService, deps Dependencies) *Module {
	return &Module{service: service, deps: deps}
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
	RegisterSettings(api, m.service, m.deps.Search, m.deps.ProxyRemoteJSON, m.deps.Config)
}
