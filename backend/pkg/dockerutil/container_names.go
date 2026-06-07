package docker

import (
	"strings"
)

// ContainerNameFromNames returns Docker's first container name without the
// leading slash Docker stores in container summaries.
func ContainerNameFromNames(names []string) string {
	if len(names) == 0 {
		return ""
	}

	return strings.TrimPrefix(names[0], "/")
}
