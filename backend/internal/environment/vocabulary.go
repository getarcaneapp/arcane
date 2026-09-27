package environment

import (
	"strings"

	kit "go.getarcane.app/kit/pkg"
)

// LocalEnvironmentID is the reserved ID of the environment Arcane manages directly.
const (
	LocalEnvironmentID                   = "0"
	localEnvironmentFallbackNameInternal = "Local"
)

// DisplayName returns the stored environment name or its readable fallback.
func DisplayName(environmentID, storedName string) string {
	if name := strings.TrimSpace(storedName); name != "" {
		return name
	}
	id := strings.TrimSpace(environmentID)
	return kit.Ternary(id == "" || id == LocalEnvironmentID, localEnvironmentFallbackNameInternal, id)
}
