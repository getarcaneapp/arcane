package utils

import (
	"strconv"
	"strings"
)

// StringOrDefault returns the trimmed value if non-empty, otherwise defaultValue.
func StringOrDefault(value, defaultValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue
	}
	return value
}

// BoolOrDefault parses value as a bool, falling back to defaultValue when empty or unparseable.
func BoolOrDefault(value string, defaultValue bool) bool {
	if value == "" {
		return defaultValue
	}
	v, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return v
}

// IntOrDefault parses value as an int, falling back to defaultValue when empty or unparseable.
func IntOrDefault(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return v
}
