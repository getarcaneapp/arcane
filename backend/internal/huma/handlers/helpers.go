package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
)

// checkAdmin checks if the current user is an admin and returns a 403 error if not.
func checkAdmin(ctx context.Context) error {
	if !humamw.IsAdminFromContext(ctx) {
		return huma.Error403Forbidden("admin access required")
	}
	return nil
}

// buildPaginationParams converts query parameters to pagination.QueryParams.
// It supports both the legacy nested style (page/limit) and the standard style (start/limit).
// A limit of -1 means "show all items" (no pagination).
func buildPaginationParams(page, start, limit int, sortCol, sortDir, search string) pagination.QueryParams {
	// limit = -1 means "show all", preserve it; zero or other negative values default to 20
	if limit < -1 {
		limit = 20
	}

	finalStart := start
	if page > 1 && start == 0 && limit > 0 {
		// Convert page-based to offset-based if page is provided and start is 0
		// Skip this conversion when limit is -1 (show all)
		finalStart = (page - 1) * limit
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: search,
		},
		SortParams: pagination.SortParams{
			Sort:  sortCol,
			Order: pagination.SortOrder(sortDir),
		},
		PaginationParams: pagination.PaginationParams{
			Start: finalStart,
			Limit: limit,
		},
		Filters: make(map[string]string),
	}
	return params
}
