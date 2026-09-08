package utils

import "strings"

// ParseBool recognizes true/1/yes/on and false/0/no/off, ignoring case and whitespace.
// The second result reports whether the value is recognized.
// This will be moved into go.getarcane.app/kit at some point.
func ParseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	default:
		return false, false
	}
}
