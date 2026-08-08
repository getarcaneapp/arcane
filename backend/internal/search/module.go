// Package search owns the metadata-backed settings and customization search indexes
// and the HTTP surface for customization search.
package search

import "github.com/danielgtaylor/huma/v2"

// Module owns both search indexes and mounts the customization search routes.
type Module struct {
	settings  *SettingsSearchService
	customize *CustomizeSearchService
}

// New builds the settings and customization search indexes.
func New() *Module {
	return &Module{
		settings:  NewSettingsSearchService(),
		customize: NewCustomizeSearchService(),
	}
}

// SettingsService exposes the settings search index to the settings domain.
func (m *Module) SettingsService() *SettingsSearchService {
	if m == nil {
		return nil
	}
	return m.settings
}

// CustomizeService exposes the customization search index to direct collaborators.
func (m *Module) CustomizeService() *CustomizeSearchService {
	if m == nil {
		return nil
	}
	return m.customize
}

// RegisterRoutes mounts customization search endpoints for runtime and schema discovery.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterCustomize(api, nil)
		return
	}
	RegisterCustomize(api, m.customize)
}
