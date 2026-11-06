package pagination

type (
	Params struct {
		Start     int
		Limit     int
		SkipCount bool
	}
)

const (
	UnknownTotal int64 = -1
)

func paginateItemsFunction[T any](items []T, params Params) []T {
	if params.Limit <= 0 {
		return items
	}

	itemsCount := len(items)

	start := min(max(params.Start, 0), itemsCount)

	end := min(start+params.Limit, itemsCount)

	return items[start:end]
}
