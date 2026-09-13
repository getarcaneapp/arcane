// Package features defines runtime features and their settings contracts.
package features

// ID identifies a feature independently of permissions and build options.
type ID string

// VulnerabilityManagement identifies scanning, reports, and scan-based patching.
const VulnerabilityManagement ID = "vulnerabilityManagement"

// VulnerabilityManagementSettingKey is the persisted toggle for vulnerability management.
const VulnerabilityManagementSettingKey = "featureVulnerabilityManagementEnabled"

// Definition connects a runtime feature to its persisted setting and default.
type Definition struct {
	ID             ID
	SettingKey     string
	DefaultEnabled bool
}

var registryInternal = [...]Definition{
	{ID: VulnerabilityManagement, SettingKey: VulnerabilityManagementSettingKey, DefaultEnabled: true},
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
