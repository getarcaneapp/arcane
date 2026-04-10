package pagination

type PaginationParams struct {
	Start int
	Limit int
}

func paginateItemsFunction[T any](items []T, params PaginationParams) []T {
	if params.Limit <= 0 {
		return items
	}

	itemsCount := len(items)

	start := min(max(params.Start, 0), itemsCount)

	end := min(start+params.Limit, itemsCount)

	return items[start:end]
}
