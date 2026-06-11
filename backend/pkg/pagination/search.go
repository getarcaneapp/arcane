package pagination

import (
	"strings"
)

// SearchAccessor extracts a searchable string from T. Return any error to skip
// the field (e.g. when matching an unknown enum state).
//
// Note: returning ("", nil) will match!
type SearchAccessor[T any] = func(T) (string, error)

type SearchQuery struct {
	Search string
}

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
