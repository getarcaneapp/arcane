package pagination

import "slices"

type (
	SortOrder string

	SortParams struct {
		Sort  string
		Order SortOrder
	}

	SortOption[T any]  func(a, b T) int
	SortBinding[T any] struct {
		Fn     SortOption[T]
		DescFn SortOption[T]
		Key    string
	}
)

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

func sortFunction[T any](items []T, params SortParams, sorts []SortBinding[T]) []T {
	if len(sorts) == 0 {
		return items
	}

	binding := sorts[0]
	for _, sort := range sorts {
		if sort.Key == params.Sort {
			binding = sort
			break
		}
	}

	fn := binding.Fn
	if params.Order == SortDesc {
		if binding.DescFn != nil {
			fn = binding.DescFn
		} else {
			fn = reverSortFn(fn)
		}
	}
	if binding.Key != sorts[0].Key {
		fn = chainSortFn(fn, sorts[0].Fn)
	}
	slices.SortStableFunc(items, fn)
	return items
}

func chainSortFn[T any](primary, tieBreak SortOption[T]) SortOption[T] {
	return func(a, b T) int {
		if c := primary(a, b); c != 0 {
			return c
		}
		return tieBreak(a, b)
	}
}

func reverSortFn[T any](fn SortOption[T]) SortOption[T] {
	return func(a, b T) int {
		return -1 * fn(a, b)
	}
}
