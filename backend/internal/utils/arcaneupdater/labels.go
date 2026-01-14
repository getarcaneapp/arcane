package arcaneupdater

import "strings"

const (
	// Core labels
	LabelArcane  = "com.getarcaneapp.arcane"         // Identifies the Arcane container itself
	LabelUpdater = "com.getarcaneapp.arcane.updater" // Enable/disable updates (true/false)

	// Dependency labels
	LabelDependsOn  = "com.getarcaneapp.arcane.depends-on"  // Comma-separated list of container names this depends on
	LabelStopSignal = "com.getarcaneapp.arcane.stop-signal" // Custom stop signal (e.g., SIGINT)
)

// IsArcaneContainer checks if the container is the Arcane application itself
func IsArcaneContainer(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, LabelArcane) {
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "true", "1", "yes", "on":
				return true
			}
		}
	}
	return false
}

// IsUpdateDisabled returns true if the special label is present and evaluates to false.
// Accepts false/0/no/off (case-insensitive) as "disabled". Default is enabled.
func IsUpdateDisabled(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, LabelUpdater) {
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "false", "0", "no", "off":
				return true
			default:
				return false
			}
		}
	}
	return false
}

// GetStopSignal returns the custom stop signal if set, otherwise empty string
func GetStopSignal(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	for k, v := range labels {
		if strings.EqualFold(k, LabelStopSignal) {
			return strings.TrimSpace(strings.ToUpper(v))
		}
	}
	return ""
}
