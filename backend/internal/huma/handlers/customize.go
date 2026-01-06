package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/category"
	"github.com/getarcaneapp/arcane/types/search"
)

// CustomizeHandler handles customization search endpoints.
type CustomizeHandler struct {
	customizeSearchService *services.CustomizeSearchService
}

// --- Input/Output Types ---

type SearchCustomizeInput struct {
	Body search.Request
}

type SearchCustomizeOutput struct {
	Body search.Response
}

type GetCustomizeCategoriesInput struct{}

type GetCustomizeCategoriesOutput struct {
	Body []category.Category
}

// RegisterCustomize registers customization endpoints using Huma.
func RegisterCustomize(api huma.API, customizeSearchService *services.CustomizeSearchService) {
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
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.Search)

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
	}, h.GetCategories)
}

// Search searches customization options by query.
func (h *CustomizeHandler) Search(ctx context.Context, input *SearchCustomizeInput) (*SearchCustomizeOutput, error) {
	if h.customizeSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest((&common.QueryParameterRequiredError{}).Error())
	}

	results := h.customizeSearchService.Search(input.Body.Query)

	if !humamw.IsAdminFromContext(ctx) {
		filtered := []category.Category{}
		for _, cat := range results.Results {
			if cat.ID != "registries" && cat.ID != "variables" {
				filtered = append(filtered, cat)
			}
		}
		results.Results = filtered
		results.Count = len(filtered)
	}

	return &SearchCustomizeOutput{
		Body: results,
	}, nil
}

// GetCategories returns all available customization categories.
func (h *CustomizeHandler) GetCategories(ctx context.Context, input *GetCustomizeCategoriesInput) (*GetCustomizeCategoriesOutput, error) {
	if h.customizeSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	categories := h.customizeSearchService.GetCustomizeCategories()

	if !humamw.IsAdminFromContext(ctx) {
		filtered := []category.Category{}
		for _, cat := range categories {
			if cat.ID != "registries" && cat.ID != "variables" {
				filtered = append(filtered, cat)
			}
		}
		categories = filtered
	}

	return &GetCustomizeCategoriesOutput{
		Body: categories,
	}, nil
}
