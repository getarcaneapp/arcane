package pagination

import "strings"

type FilterResult[T any] struct {
	Items          []T
	TotalCount     int64
	TotalAvailable int64
}

type FilterAccessor[T any] struct {
	Key string
	Fn  func(item T, filterValue string) bool
}

type Config[T any] struct {
	SearchAccessors []SearchAccessor[T]
	// SortBindings order matters: the first binding is the default sort when the
	// requested key is empty or unknown, and serves as the tie-break for every
	// other binding so pagination is deterministic.
	SortBindings    []SortBinding[T]
	FilterAccessors []FilterAccessor[T]
}

func (c Config[T]) SearchOrderAndPaginate(items []T, params QueryParams) FilterResult[T] {
	totalAvailable := len(items)

	items = searchFn(items, params.SearchQuery, c.SearchAccessors)
	items = filterFn(items, params.Filters, c.FilterAccessors)

	return FilterResult[T]{
		Items:          c.OrderAndPaginate(items, params),
		TotalCount:     int64(len(items)),
		TotalAvailable: int64(totalAvailable),
	}
}

// OrderAndPaginate sorts items and slices the requested page without applying
// the search term or filters — for callers that already filtered while
// building the item list (e.g. dropping non-matches during an incremental
// decode) and only need ordering plus the page cut.
func (c Config[T]) OrderAndPaginate(items []T, params QueryParams) []T {
	items = sortFunction(items, params.SortParams, c.SortBindings)
	return paginateItemsFunction(items, params.Params)
}

// MatchesSearchAndFilters reports whether a single item passes params' search
// term and filters under the config — the same predicate SearchOrderAndPaginate
// applies. It lets callers that decode large payloads incrementally drop
// non-matching items as they go instead of materializing everything first.
func (c Config[T]) MatchesSearchAndFilters(item T, params QueryParams) bool {
	search := strings.ToLower(strings.TrimSpace(params.Search))
	if search != "" {
		matched := false
		for _, accessor := range c.SearchAccessors {
			value, err := accessor(item)
			if err == nil && strings.Contains(strings.ToLower(value), search) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(params.Filters) == 0 {
		return true
	}
	return itemMatches(item, params.Filters, c.FilterAccessors)
}

func filterFn[T any](items []T, filters map[string]string, accessors []FilterAccessor[T]) []T {
	if len(filters) == 0 {
		return items
	}

	results := []T{}
	for _, item := range items {
		if itemMatches(item, filters, accessors) {
			results = append(results, item)
		}
	}
	return results
}

func itemMatches[T any](item T, filters map[string]string, accessors []FilterAccessor[T]) bool {
	for key, value := range filters {
		accessor := getAccessor(key, accessors)
		if accessor == nil {
			return false
		}

		if !matchValue(item, value, accessor) {
			return false
		}
	}
	return true
}

func getAccessor[T any](key string, accessors []FilterAccessor[T]) *FilterAccessor[T] {
	for i := range accessors {
		if accessors[i].Key == key {
			return &accessors[i]
		}
	}
	return nil
}

func matchValue[T any](item T, value string, accessor *FilterAccessor[T]) bool {
	if strings.Contains(value, ",") {
		values := strings.SplitSeq(value, ",")
		for v := range values {
			v = strings.TrimSpace(v)
			if accessor.Fn(item, v) {
				return true
			}
		}
		return false
	}
	return accessor.Fn(item, value)
}
