package updater

import "strings"

const (
	// Core labels
	LabelArcane      = "com.getarcaneapp.arcane"         // Identifies the Arcane container itself
	LabelArcaneAgent = "com.getarcaneapp.arcane.agent"   // Identifies an Arcane agent container
	LabelUpdater     = "com.getarcaneapp.arcane.updater" // Enable/disable updates (true/false)

	// Dependency labels
	LabelDependsOn  = "com.getarcaneapp.arcane.depends-on"  // Comma-separated list of container names this depends on
	LabelStopSignal = "com.getarcaneapp.arcane.stop-signal" // Custom stop signal (e.g., SIGINT)
)

// IsArcaneContainer checks if the container is the Arcane application itself
func IsArcaneContainer(labels map[string]string) bool {
	return hasTruthyLabelInternal(labels, LabelArcane) || IsArcaneAgentContainer(labels)
}

// IsArcaneServerContainer checks if the container is the Arcane server, excluding agents.
func IsArcaneServerContainer(labels map[string]string) bool {
	return hasTruthyLabelInternal(labels, LabelArcane) && !IsArcaneAgentContainer(labels)
}

// ShouldDisableArcaneServerRedeploy reports whether redeploy should be blocked for the given container.
func ShouldDisableArcaneServerRedeploy(labels map[string]string, containerID, currentContainerID string, currentErr error) bool {
	if !IsArcaneServerContainer(labels) {
		return false
	}

	if currentErr != nil || strings.TrimSpace(currentContainerID) == "" {
		// Without a runtime container ID, non-agent Arcane server labels are treated as protected.
		// This prevents accidental self-redeploy, but native/CI runs can mark other Arcane servers disabled.
		return true
	}

	return containerIDsMatchInternal(containerID, currentContainerID)
}

// IsArcaneAgentContainer checks if the container is an Arcane agent container.
func IsArcaneAgentContainer(labels map[string]string) bool {
	return hasTruthyLabelInternal(labels, LabelArcaneAgent)
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

func hasTruthyLabelInternal(labels map[string]string, target string) bool {
	if labels == nil {
		return false
	}

	for k, v := range labels {
		if strings.EqualFold(k, target) && isTruthyLabelValueInternal(v) {
			return true
		}
	}

	return false
}

func isTruthyLabelValueInternal(v string) bool {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func containerIDsMatchInternal(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}

	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
