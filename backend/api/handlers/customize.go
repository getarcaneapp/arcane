package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
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

var customizeCategoryPermissionsInternal = map[string][]string{
	"templates":        {authz.PermTemplatesList, authz.PermTemplatesRead},
	"registries":       {authz.PermRegistriesList, authz.PermRegistriesRead},
	"variables":        {authz.PermTemplatesRead},
	"git-repositories": {authz.PermGitReposList, authz.PermGitReposRead},
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

func canAccessCustomizeCategoryInternal(ps *authz.PermissionSet, categoryID string) bool {
	if ps == nil {
		return false
	}
	if ps.Allows(authz.PermCustomizeManage, "") {
		return true
	}
	perms, ok := customizeCategoryPermissionsInternal[categoryID]
	if !ok {
		return false
	}
	for _, perm := range perms {
		if ps.Allows(perm, "") {
			return true
		}
	}
	return false
}

func filterCustomizeCategoriesInternal(ps *authz.PermissionSet, categories []category.Category) []category.Category {
	if ps == nil {
		return []category.Category{}
	}
	filtered := make([]category.Category, 0, len(categories))
	for _, cat := range categories {
		if canAccessCustomizeCategoryInternal(ps, cat.ID) {
			filtered = append(filtered, cat)
		}
	}
	return filtered
}

// Search searches customization options by query.
func (h *CustomizeHandler) Search(ctx context.Context, input *SearchCustomizeInput) (*SearchCustomizeOutput, error) {
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

	return &SearchCustomizeOutput{
		Body: results,
	}, nil
}

// GetCategories returns all available customization categories.
func (h *CustomizeHandler) GetCategories(ctx context.Context, input *GetCustomizeCategoriesInput) (*GetCustomizeCategoriesOutput, error) {
	if h.customizeSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	categories := filterCustomizeCategoriesInternal(ps, h.customizeSearchService.GetCustomizeCategories())

	return &GetCustomizeCategoriesOutput{
		Body: categories,
	}, nil
}
