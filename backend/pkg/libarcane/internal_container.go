package libarcane

import "strings"

// Internal containers indicate containers used for arcanes utilties, ie: temp containers used for viewing files for volumes etc
const InternalContainerLabel = "com.getarcaneapp.internal.container"

func IsInternalContainer(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k, v := range labels {
		if strings.EqualFold(k, InternalContainerLabel) {
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "true", "1", "yes", "on":
				return true
			}
		}
	}
	return false
}
