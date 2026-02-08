package pagination

type Response struct {
	TotalPages      int64 `json:"totalPages"`
	TotalItems      int64 `json:"totalItems"`
	CurrentPage     int   `json:"currentPage"`
	ItemsPerPage    int   `json:"itemsPerPage"`
	GrandTotalItems int64 `json:"grandTotalItems,omitempty"`
}

// BuildResponseFromFilterResult creates a pagination Response from a FilterResult.
// Handles the special case where limit = -1 means "show all".
func BuildResponseFromFilterResult[T any](result FilterResult[T], params QueryParams) Response {
	limit := params.Limit
	totalCount := result.TotalCount

	var totalPages int64
	var page int
	var itemsPerPage int

	switch {
	case limit == -1:
		// "Show all" mode: single page with all items
		totalPages = 1
		page = 1
		itemsPerPage = int(totalCount)
	case limit > 0:
		totalPages = (totalCount + int64(limit) - 1) / int64(limit)
		page = (params.Start / limit) + 1
		itemsPerPage = limit
	default:
		// Fallback for invalid limit values
		totalPages = 1
		page = 1
		itemsPerPage = int(totalCount)
	}

	if totalPages == 0 {
		totalPages = 1
	}

	return Response{
		TotalPages:      totalPages,
		TotalItems:      totalCount,
		CurrentPage:     page,
		ItemsPerPage:    itemsPerPage,
		GrandTotalItems: result.TotalAvailable,
	}
}
