package libarcane

import "strings"

// Internal containers indicate containers used for arcanes utilties, ie: temp containers used for viewing files for volumes etc
const (
	InternalResourceLabel = "com.getarcaneapp.internal.resource"
	// Deprecated - Legacy label. Use InternalResourceLabel instead.
	legacyInternalContainerLabel = "com.getarcaneapp.internal.container"
)

func IsInternalContainer(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, InternalResourceLabel) || strings.EqualFold(k, legacyInternalContainerLabel) {
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "true", "1", "yes", "on":
				return true
			}
		}
	}
	return false
}
