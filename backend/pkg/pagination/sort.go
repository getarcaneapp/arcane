package pagination

import "slices"

type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

type SortParams struct {
	Sort  string
	Order SortOrder
}

type (
	SortOption[T any]  func(a, b T) int
	SortBinding[T any] struct {
		Key string
		Fn  SortOption[T]
	}
)

func sortFunction[T any](items []T, params SortParams, sorts []SortBinding[T]) []T {
	for _, sort := range sorts {
		if sort.Key == params.Sort {
			fn := sort.Fn
			if params.Order == SortDesc {
				fn = reverSortFn(fn)
			}
			slices.SortStableFunc(items, fn)
		}
	}
	return items
}

func reverSortFn[T any](fn SortOption[T]) SortOption[T] {
	return func(a, b T) int {
		return -1 * fn(a, b)
	}
}
