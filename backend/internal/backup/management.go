package backup

import (
	"strings"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
)

// ParseManagementTypeFilter returns the single recognized management type in a comma-separated filter.
func ParseManagementTypeFilter(filter string) (backuptypes.ManagementType, bool) {
	selected := make(map[backuptypes.ManagementType]struct{}, 2)
	for value := range strings.SplitSeq(filter, ",") {
		managementType := backuptypes.ManagementType(strings.TrimSpace(value))
		if managementType == backuptypes.ManagementTypeSystem || managementType == backuptypes.ManagementTypeVolume {
			selected[managementType] = struct{}{}
		}
	}
	if len(selected) != 1 {
		return "", false
	}
	for managementType := range selected {
		return managementType, true
	}
	return "", false
}
