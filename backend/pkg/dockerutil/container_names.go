package docker

import (
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// ContainerNameFromNames returns Docker's first container name without the
// leading slash Docker stores in container summaries.
func ContainerNameFromNames(names []string) string {
	if len(names) == 0 {
		return ""
	}

	return strings.TrimPrefix(names[0], "/")
}

// ExcludedContainerNameSet parses a comma-separated exclusion setting into a
// name lookup. Returns nil when the setting names no containers.
func ExcludedContainerNameSet(raw string) map[string]bool {
	names := utils.UniqueNonEmptyStrings(strings.Split(raw, ","))
	if len(names) == 0 {
		return nil
	}
	excluded := make(map[string]bool, len(names))
	for _, name := range names {
		excluded[name] = true
	}
	return excluded
}

// ContainerNameExcluded reports whether any of Docker's container names,
// stripped of the leading slash, appears in the exclusion lookup.
func ContainerNameExcluded(names []string, excluded map[string]bool) bool {
	if len(excluded) == 0 {
		return false
	}
	for _, name := range names {
		if excluded[strings.TrimPrefix(name, "/")] {
			return true
		}
	}
	return false
}
