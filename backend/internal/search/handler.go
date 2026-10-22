package search

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

// CustomizeHandler handles customization search endpoints.
type CustomizeHandler struct {
	customizeSearchService *CustomizeSearchService
}

type SearchCustomizeInput struct {
	Body types.SearchRequest
}

type SearchCustomizeOutput struct {
	Body types.SearchResponse
}

type GetCustomizeCategoriesInput struct{}

type GetCustomizeCategoriesOutput struct {
	Body []types.Category
}

func filterCustomizeCategories(ps *authz.PermissionSet, categories []types.Category) []types.Category {
	if ps == nil {
		return []types.Category{}
	}
	filtered := make([]types.Category, 0, len(categories))
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

	ps, _ := authz.PermissionsFromContext(ctx)
	results := Search(h.customizeSearchService.GetCustomizeCategories(), input.Body.Query, types.SearchProfileCustomize)
	results.Results = filterCustomizeCategories(ps, results.Results)
	results.Count = len(results.Results)

	return &SearchCustomizeOutput{
		Body: results,
	}, nil
}

// GetCategories returns all available customization categories.
func (h *CustomizeHandler) GetCategories(ctx context.Context, input *GetCustomizeCategoriesInput) (*GetCustomizeCategoriesOutput, error) {
	ps, _ := authz.PermissionsFromContext(ctx)
	categories := filterCustomizeCategories(ps, h.customizeSearchService.GetCustomizeCategories())

	return &GetCustomizeCategoriesOutput{
		Body: categories,
	}, nil
}
