package utils

import "strings"

// StringOrDefault returns the trimmed value if non-empty, otherwise defaultValue.
func StringOrDefault(value, defaultValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue
	}
	return value
}
