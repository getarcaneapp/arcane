// Package containerlabel looks up Arcane's container labels.
package containerlabel

import (
	"strings"

	"kit.getarcane.app"
)

// AutoHealLabel opts a container in to auto-heal, which the auto-heal opt-in
// mode requires.
const AutoHealLabel = "com.getarcaneapp.arcane.auto-heal"

// Value looks up name, accepting other spellings; among conflicting ones the
// smallest key wins so the answer does not depend on map iteration order.
func Value(labels map[string]string, name string) (string, bool) {
	value, ok := labels[name]
	if ok {
		return value, true
	}
	var matched string
	for key, candidate := range labels {
		if strings.EqualFold(strings.TrimSpace(key), name) && (!ok || key < matched) {
			matched, value, ok = key, candidate, true
		}
	}
	return value, ok
}

// IsEnabled reports whether the label name is present with a true value.
func IsEnabled(labels map[string]string, name string) bool {
	value, ok := Value(labels, name)
	if !ok {
		return false
	}
	enabled, parsed := kit.ParseBool(value)
	return parsed && enabled
}
