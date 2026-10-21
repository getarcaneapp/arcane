package types

// FeatureID identifies a feature independently of permissions and build options.
type FeatureID string

const (
	// FeatureVulnerabilityManagement identifies scanning, reports, and scan-based patching.
	FeatureVulnerabilityManagement FeatureID = "vulnerabilityManagement"

	// FeatureVulnerabilityManagementSettingKey is the persisted toggle for vulnerability management.
	FeatureVulnerabilityManagementSettingKey = "featureVulnerabilityManagementEnabled"

	// FeatureSwarm identifies the Docker Swarm pages. An active swarm cluster keeps it enabled
	// regardless of the persisted toggle.
	FeatureSwarm FeatureID = "swarm"

	// FeatureSwarmSettingKey is the persisted toggle for Docker Swarm.
	FeatureSwarmSettingKey = "featureSwarmEnabled"
)

// FeatureDefinition connects a runtime feature to its persisted setting and default.
type FeatureDefinition struct {
	ID             FeatureID
	SettingKey     string
	DefaultEnabled bool
}

var featureRegistry = [...]FeatureDefinition{
	{ID: FeatureVulnerabilityManagement, SettingKey: FeatureVulnerabilityManagementSettingKey, DefaultEnabled: true},
	{ID: FeatureSwarm, SettingKey: FeatureSwarmSettingKey, DefaultEnabled: false},
}

// AllFeatures returns the supported runtime features.
func AllFeatures() []FeatureDefinition {
	return append([]FeatureDefinition{}, featureRegistry[:]...)
}

// LookupFeature returns the definition of a supported runtime feature.
func LookupFeature(id FeatureID) (FeatureDefinition, bool) {
	for _, definition := range featureRegistry {
		if definition.ID == id {
			return definition, true
		}
	}
	return FeatureDefinition{}, false
}

// FeatureDisabledError describes an unavailable runtime feature using problem details.
type FeatureDisabledError struct {
	Status  int       `json:"status"`
	Title   string    `json:"title"`
	Detail  string    `json:"detail"`
	Code    string    `json:"code"`
	Feature FeatureID `json:"feature"`
}

// Error returns the reason the feature operation was rejected.
func (e *FeatureDisabledError) Error() string { return e.Detail }

// GetStatus supplies the HTTP response status to Huma.
func (e *FeatureDisabledError) GetStatus() int { return e.Status }

// ContentType selects the problem-details media type for JSON and CBOR responses.
func (e *FeatureDisabledError) ContentType(contentType string) string {
	switch contentType {
	case "application/json":
		return "application/problem+json"
	case "application/cbor":
		return "application/problem+cbor"
	default:
		return contentType
	}
}
