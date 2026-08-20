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
		Key    string
		Fn     SortOption[T]
		DescFn SortOption[T]
	}
)

// sortFunction always sorts: when params.Sort is empty or matches no binding it
// falls back to the first binding, and when a non-first binding is used its ties
// are broken by the first binding's ascending comparator. Pagination slices the
// sorted result by index, so an unsorted (or tie-ambiguous) order would make
// page walks skip and duplicate items whenever the source order shifts between
// requests (e.g. Docker's map-iteration order for volumes and networks).
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

// chainSortFn falls through to tieBreak when primary considers two items equal.
// The tie-break is applied after any desc adjustment and always ascending: its
// job is a deterministic total order, not a meaningful secondary sort.
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
