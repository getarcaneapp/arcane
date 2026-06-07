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
	SortBindings    []SortBinding[T]
	FilterAccessors []FilterAccessor[T]
}

func SearchOrderAndPaginate[T any](items []T, params QueryParams, searchConfig Config[T]) FilterResult[T] {
	totalAvailable := len(items)

	items = searchFn(items, params.SearchQuery, searchConfig.SearchAccessors)
	items = filterFn(items, params.Filters, searchConfig.FilterAccessors)
	items = sortFunction(items, params.SortParams, searchConfig.SortBindings)

	totalCount := len(items)
	items = paginateItemsFunction(items, params.PaginationParams)

	return FilterResult[T]{
		Items:          items,
		TotalCount:     int64(totalCount),
		TotalAvailable: int64(totalAvailable),
	}
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
