package docker

import (
	"strings"

	"github.com/moby/moby/api/types/container"
)

// ContainerNameFromNames returns Docker's first container name without the
// leading slash Docker stores in container summaries.
func ContainerNameFromNames(names []string) string {
	if len(names) == 0 {
		return ""
	}

	return strings.TrimPrefix(names[0], "/")
}

// ContainerSummaryName returns a displayable container name from a Docker
// summary, falling back to the short container ID when Docker has no name.
func ContainerSummaryName(cnt container.Summary) string {
	if name := ContainerNameFromNames(cnt.Names); name != "" {
		return name
	}

	if len(cnt.ID) >= 12 {
		return cnt.ID[:12]
	}
	return cnt.ID
}
