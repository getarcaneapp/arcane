package pagination

import (
	"strings"
)

type (
	SearchAccessor[T any] = func(T) (string, error)

	SearchQuery struct {
		Search string
	}
)

func searchFn[T any](items []T, params SearchQuery, accessors []SearchAccessor[T]) []T {
	search := strings.ToLower(strings.TrimSpace(params.Search))

	if search == "" {
		return items
	}

	results := []T{}

	for iIdx := range items {
		for aIdx := range accessors {
			value, err := accessors[aIdx](items[iIdx])
			if err == nil && strings.Contains(strings.ToLower(value), search) {
				results = append(results, items[iIdx])
				break
			}
		}
	}

	return results
}
