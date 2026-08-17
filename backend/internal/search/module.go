// Package search owns the metadata-backed settings and customization search indexes
// and the HTTP surface for customization search.
package search

import "github.com/danielgtaylor/huma/v2"

// Module owns the customization search index and mounts the customization search routes.
type Module struct {
	customize *CustomizeSearchService
}

// New builds the customization search index.
func New() *Module {
	return &Module{
		customize: NewCustomizeSearchService(),
	}
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
