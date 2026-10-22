// Package search owns the metadata-backed settings and customization search indexes
// and the HTTP surface for customization search.
package search

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

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

// RegisterCustomize registers customization endpoints using Huma.
func RegisterCustomize(api huma.API, customizeSearchService *CustomizeSearchService) {
	h := &CustomizeHandler{
		customizeSearchService: customizeSearchService,
	}

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "search-customize",
		Method:      http.MethodPost,
		Path:        "/customize/search",
		Summary:     "Search customization options",
		Description: "Search customization categories and options by query",
		Tags:        []string{"Customize"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.Search)

	middleware.RegisterAuthenticated(api, huma.Operation{
		OperationID: "get-customize-categories",
		Method:      http.MethodGet,
		Path:        "/customize/categories",
		Summary:     "Get customization categories",
		Description: "Get all available customization categories with metadata",
		Tags:        []string{"Customize"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetCategories)
}
