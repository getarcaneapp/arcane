package pagination

import (
	"fmt"
	"strings"
)

type (
	FilterResult[T any] struct {
		Items          []T
		TotalCount     int64
		TotalAvailable int64
	}

	FilterAccessor[T any] struct {
		Fn  func(item T, filterValue string) bool
		Key string
		// NoSplit passes the raw value through instead of treating commas as alternatives.
		NoSplit bool
	}

	Config[T any] struct {
		SearchAccessors []SearchAccessor[T]
		SortBindings    []SortBinding[T]
		FilterAccessors []FilterAccessor[T]
	}
)

func LabelFilter[T, V any](labelsOf func(T) map[string]V) FilterAccessor[T] {
	return FilterAccessor[T]{
		Fn: func(item T, filterValue string) bool {
			key, value, hasValue := strings.Cut(filterValue, "=")
			if strings.TrimSpace(key) == "" {
				return true
			}
			actual, ok := labelsOf(item)[strings.TrimSpace(key)]
			return ok && (!hasValue || fmt.Sprint(actual) == value)
		},
		Key:     "label",
		NoSplit: true,
	}
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

func (c Config[T]) OrderAndPaginate(items []T, params QueryParams) []T {
	items = sortFunction(items, params.SortParams, c.SortBindings)
	return paginateItemsFunction(items, params.Params)
}

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
	if !accessor.NoSplit && strings.Contains(value, ",") {
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
