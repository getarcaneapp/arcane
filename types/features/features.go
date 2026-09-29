// Package features defines runtime features and their settings contracts.
package features

// ID identifies a feature independently of permissions and build options.
type ID string

const (
	// VulnerabilityManagement identifies scanning, reports, and scan-based patching.
	VulnerabilityManagement ID = "vulnerabilityManagement"

	// VulnerabilityManagementSettingKey is the persisted toggle for vulnerability management.
	VulnerabilityManagementSettingKey = "featureVulnerabilityManagementEnabled"

	// Swarm identifies the Docker Swarm pages. An active swarm cluster keeps it enabled
	// regardless of the persisted toggle.
	Swarm ID = "swarm"

	// SwarmSettingKey is the persisted toggle for Docker Swarm.
	SwarmSettingKey = "featureSwarmEnabled"
)

// Definition connects a runtime feature to its persisted setting and default.
type Definition struct {
	ID             ID
	SettingKey     string
	DefaultEnabled bool
}

var registryInternal = [...]Definition{
	{ID: VulnerabilityManagement, SettingKey: VulnerabilityManagementSettingKey, DefaultEnabled: true},
	{ID: Swarm, SettingKey: SwarmSettingKey, DefaultEnabled: false},
}

// All returns the supported runtime features.
func All() []Definition {
	return append([]Definition{}, registryInternal[:]...)
}

// Lookup returns the definition of a supported runtime feature.
func Lookup(id ID) (Definition, bool) {
	for _, definition := range registryInternal {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}
