package handlers

import (
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
)

// buildPaginationParams converts query parameters to pagination.QueryParams.
func buildPaginationParams(page, limit int, sortCol, sortDir string) pagination.QueryParams {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	// Convert page-based to offset-based
	start := (page - 1) * limit
	params := pagination.QueryParams{
		SortParams: pagination.SortParams{
			Sort:  sortCol,
			Order: pagination.SortOrder(sortDir),
		},
		PaginationParams: pagination.PaginationParams{
			Start: start,
			Limit: limit,
		},
		Filters: make(map[string]string),
	}
	return params
}
