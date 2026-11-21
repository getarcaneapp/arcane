package pagination

import "kit.getarcane.app"

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

func (p Params) FetchWindow() int {
	if p.Limit == -1 {
		return -1
	}
	return p.Start + p.Limit + kit.Ternary(p.SkipCount, 1, 0)
}

func paginateItemsFunction[T any](items []T, params Params) []T {
	if params.Limit <= 0 {
		return items
	}

	itemsCount := len(items)

	start := min(max(params.Start, 0), itemsCount)

	end := min(start+params.Limit, itemsCount)

	return items[start:end]
}
