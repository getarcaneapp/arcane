package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/category"
	"github.com/getarcaneapp/arcane/types/v2/search"
)

// customizeHandler handles customization search endpoints.
type customizeHandler struct {
	customizeSearchService *services.CustomizeSearchService
}

// --- Input/Output Types ---

type searchCustomizeInput struct {
	Body search.Request
}

type searchCustomizeOutput struct {
	Body search.Response
}

type getCustomizeCategoriesInput struct{}

type getCustomizeCategoriesOutput struct {
	Body []category.Category
}

// RegisterCustomize registers customization endpoints using Huma.
func RegisterCustomize(api huma.API, customizeSearchService *services.CustomizeSearchService) {
	h := &customizeHandler{
		customizeSearchService: customizeSearchService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "search-customize",
		Method:      http.MethodPost,
		Path:        "/customize/search",
		Summary:     "Search customization options",
		Description: "Search customization categories and options by query",
		Tags:        []string{"Customize"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.searchInternal)

	huma.Register(api, huma.Operation{
		OperationID: "get-customize-categories",
		Method:      http.MethodGet,
		Path:        "/customize/categories",
		Summary:     "Get customization categories",
		Description: "Get all available customization categories with metadata",
		Tags:        []string{"Customize"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.getCategoriesInternal)
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
func (h *customizeHandler) searchInternal(ctx context.Context, input *searchCustomizeInput) (*searchCustomizeOutput, error) {
	if h.customizeSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest((&common.QueryParameterRequiredError{}).Error())
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	results := h.customizeSearchService.Search(input.Body.Query)
	results.Results = filterCustomizeCategoriesInternal(ps, results.Results)
	results.Count = len(results.Results)

	return &searchCustomizeOutput{
		Body: results,
	}, nil
}

// GetCategories returns all available customization categories.
func (h *customizeHandler) getCategoriesInternal(ctx context.Context, _ *getCustomizeCategoriesInput) (*getCustomizeCategoriesOutput, error) {
	if h.customizeSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	categories := filterCustomizeCategoriesInternal(ps, h.customizeSearchService.GetCustomizeCategories())

	return &getCustomizeCategoriesOutput{
		Body: categories,
	}, nil
}
