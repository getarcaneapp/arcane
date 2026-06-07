package libarcane

import "strings"

// InternalResourceLabel marks containers used for Arcane utilities, e.g. temp containers used for viewing volume files.
const InternalResourceLabel = "com.getarcaneapp.internal.resource"

func IsInternalContainer(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, InternalResourceLabel) {
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "true", "1", "yes", "on":
				return true
			}
		}
	}
	return false
}
