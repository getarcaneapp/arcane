package pagination

type PaginationParams struct {
	Start int
	Limit int
	// SkipCount opts out of the COUNT(*) query that backs TotalItems/TotalPages.
	// When true, paginated DB helpers return UnknownTotal (-1) for both fields and
	// the response is suitable for infinite-scroll UIs that don't render page numbers.
	// Default (false) preserves the historical behaviour for all existing callers.
	SkipCount bool
}

// UnknownTotal is the sentinel returned in TotalItems/TotalPages when SkipCount is set.
const UnknownTotal int64 = -1

func paginateItemsFunction[T any](items []T, params PaginationParams) []T {
	if params.Limit <= 0 {
		return items
	}

	itemsCount := len(items)

	start := min(max(params.Start, 0), itemsCount)

	end := min(start+params.Limit, itemsCount)

	return items[start:end]
}
