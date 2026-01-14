package arcaneupdater

import "strings"

const (
	// Core labels
	LabelArcane  = "com.getarcaneapp.arcane"         // Identifies the Arcane container itself
	LabelUpdater = "com.getarcaneapp.arcane.updater" // Enable/disable updates (true/false)

	// Lifecycle hook labels
	LabelPreCheck   = "com.getarcaneapp.arcane.lifecycle.pre-check"   // Command to run before checking for updates
	LabelPostCheck  = "com.getarcaneapp.arcane.lifecycle.post-check"  // Command to run after checking for updates
	LabelPreUpdate  = "com.getarcaneapp.arcane.lifecycle.pre-update"  // Command to run before stopping container
	LabelPostUpdate = "com.getarcaneapp.arcane.lifecycle.post-update" // Command to run after starting new container

	// Lifecycle timeout labels (in seconds)
	LabelPreUpdateTimeout  = "com.getarcaneapp.arcane.lifecycle.pre-update-timeout"
	LabelPostUpdateTimeout = "com.getarcaneapp.arcane.lifecycle.post-update-timeout"

	// Dependency labels
	LabelDependsOn  = "com.getarcaneapp.arcane.depends-on"  // Comma-separated list of container names this depends on
	LabelStopSignal = "com.getarcaneapp.arcane.stop-signal" // Custom stop signal (e.g., SIGINT)

	ExitCodeSkipUpdate = 75
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

// GetLifecycleCommand returns the lifecycle command for the given label, if set
func GetLifecycleCommand(labels map[string]string, lifecycleLabel string) []string {
	if labels == nil {
		return nil
	}
	for k, v := range labels {
		if strings.EqualFold(k, lifecycleLabel) {
			v = strings.TrimSpace(v)
			if v == "" {
				return nil
			}
			// Simple shell-style split (use /bin/sh -c for complex commands)
			return []string{"/bin/sh", "-c", v}
		}
	}
	return nil
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
