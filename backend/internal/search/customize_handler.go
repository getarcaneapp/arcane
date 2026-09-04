package search

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/category"
	searchtypes "github.com/getarcaneapp/arcane/types/v2/search"
)

// CustomizeHandler handles customization search endpoints.
type CustomizeHandler struct {
	customizeSearchService *CustomizeSearchService
}

// --- Input/Output Types ---

type SearchCustomizeInput struct {
	Body searchtypes.Request
}

type SearchCustomizeOutput struct {
	Body searchtypes.Response
}

type GetCustomizeCategoriesInput struct{}

type GetCustomizeCategoriesOutput struct {
	Body []category.Category
}

// RegisterCustomize registers customization endpoints using Huma.
func RegisterCustomize(api huma.API, customizeSearchService *CustomizeSearchService) {
	h := &CustomizeHandler{
		customizeSearchService: customizeSearchService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "search-customize",
		Method:      http.MethodPost,
		Path:        "/customize/search",
		Summary:     "Search customization options",
		Description: "Search customization categories and options by query",
		Tags:        []string{"Customize"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.Search)

	huma.Register(api, huma.Operation{
		OperationID: "get-customize-categories",
		Method:      http.MethodGet,
		Path:        "/customize/categories",
		Summary:     "Get customization categories",
		Description: "Get all available customization categories with metadata",
		Tags:        []string{"Customize"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetCategories)
}

func filterCustomizeCategoriesInternal(ps *authz.PermissionSet, categories []category.Category) []category.Category {
	if ps == nil {
		return []category.Category{}
	}
	filtered := make([]category.Category, 0, len(categories))
	for _, cat := range categories {
		if authz.CanAccessCustomizeCategory(ps, cat.ID, "") {
			filtered = append(filtered, cat)
		}
	}
	return filtered
}

// Search searches customization options by query.
func (h *CustomizeHandler) Search(ctx context.Context, input *SearchCustomizeInput) (*SearchCustomizeOutput, error) {
	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest("Query parameter is required")
	}

	ps, _ := middleware.PermissionsFromContext(ctx)
	results := Search(h.customizeSearchService.GetCustomizeCategories(), input.Body.Query, searchtypes.CustomizeProfile)
	results.Results = filterCustomizeCategoriesInternal(ps, results.Results)
	results.Count = len(results.Results)

	return &SearchCustomizeOutput{
		Body: results,
	}, nil
}

// GetCategories returns all available customization categories.
func (h *CustomizeHandler) GetCategories(ctx context.Context, input *GetCustomizeCategoriesInput) (*GetCustomizeCategoriesOutput, error) {
	ps, _ := middleware.PermissionsFromContext(ctx)
	categories := filterCustomizeCategoriesInternal(ps, h.customizeSearchService.GetCustomizeCategories())

	return &GetCustomizeCategoriesOutput{
		Body: categories,
	}, nil
}
