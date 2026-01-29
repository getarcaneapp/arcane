// New string utilties, will move the rest over time.
package utils

import (
	"fmt"
	"strings"
)

// ToString converts any value to a trimmed string.
func ToString(v any) string {
	if v == nil {
		return ""
	}
	var s string
	switch val := v.(type) {
	case string:
		s = val
	case fmt.Stringer:
		s = val.String()
	default:
		s = fmt.Sprint(v)
	}
	return strings.TrimSpace(s)
}

// Collect transforms a value (single item or slice) into a slice of T using a mapper.
func Collect[T any](value any, mapper func(any) T) []T {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []any:
		res := make([]T, len(v))
		for i, item := range v {
			res[i] = mapper(item)
		}
		return res
	case []string:
		res := make([]T, len(v))
		for i, s := range v {
			res[i] = mapper(s)
		}
		return res
	default:
		return []T{mapper(value)}
	}
}

// AsStringMap attempts to convert any map-like interface to map[string]any.
func AsStringMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case map[string]string:
		res := make(map[string]any, len(v))
		for k, val := range v {
			res[k] = val
		}
		return res, true
	case map[any]any:
		res := make(map[string]any, len(v))
		for k, val := range v {
			if s, ok := k.(string); ok {
				res[s] = val
			}
		}
		return res, len(res) > 0
	}
	return nil, false
}

// UniqueNonEmptyStrings returns unique, non-empty, trimmed strings.
func UniqueNonEmptyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		if _, exists := seen[s]; !exists {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// FirstNonEmpty returns the first non-empty string in a list of values.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
