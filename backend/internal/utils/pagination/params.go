package pagination

import (
	arcanehttp "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	"github.com/gin-gonic/gin"
)

type QueryParams struct {
	SearchQuery
	SortParams
	PaginationParams
	Filters map[string]string
}

func ExtractListModifiersQueryParams(c *gin.Context) QueryParams {
	// search
	search, _ := arcanehttp.GetQueryParam(c, "search", true)
	// sorting
	sortField, _ := arcanehttp.GetQueryParam(c, "sort", true)
	sortOrder, _ := arcanehttp.GetQueryParam(c, "order", true)
	// pagination
	start, _ := arcanehttp.GetIntQueryParam(c, "start", true)
	limit, _ := arcanehttp.GetIntQueryParam(c, "limit", true)

	filters := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if key != "search" && key != "sort" && key != "order" && key != "start" && key != "limit" {
			if len(values) > 0 {
				filters[key] = values[0]
			}
		}
	}

	return QueryParams{
		SearchQuery{
			Search: search,
		},
		SortParams{
			sort:  sortField,
			order: SortOrder(sortOrder),
		},
		PaginationParams{
			Start: start,
			Limit: limit,
		},
		filters,
	}
}
