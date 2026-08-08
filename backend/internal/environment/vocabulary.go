package environment

import "strings"

// LocalEnvironmentID is the reserved ID of the environment Arcane manages directly.
const LocalEnvironmentID = "0"

const localEnvironmentFallbackNameInternal = "Local"

// DisplayName returns the stored environment name or its readable fallback.
func DisplayName(environmentID, storedName string) string {
	if name := strings.TrimSpace(storedName); name != "" {
		return name
	}
	id := strings.TrimSpace(environmentID)
	if id == "" || id == LocalEnvironmentID {
		return localEnvironmentFallbackNameInternal
	}
	return id
}
