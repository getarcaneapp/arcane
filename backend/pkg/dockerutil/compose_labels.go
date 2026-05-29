package docker

import "strings"

// Docker Compose attaches these labels to every container it manages. They
// identify the owning project and the service within that project.
const (
	ComposeProjectLabelKey = "com.docker.compose.project"
	ComposeServiceLabelKey = "com.docker.compose.service"
)

// ComposeProjectLabel returns the trimmed Docker Compose project name from a
// container's labels, or "" when unset.
func ComposeProjectLabel(labels map[string]string) string {
	return strings.TrimSpace(labels[ComposeProjectLabelKey])
}

// ComposeServiceLabel returns the trimmed Docker Compose service name from a
// container's labels, or "" when unset.
func ComposeServiceLabel(labels map[string]string) string {
	return strings.TrimSpace(labels[ComposeServiceLabelKey])
}
