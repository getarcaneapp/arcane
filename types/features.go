package types

type (
	FeatureID string

	FeatureDefinition struct {
		ID             FeatureID
		SettingKey     string
		DefaultEnabled bool
	}

	FeatureDisabledError struct {
		Title   string    `json:"title"`
		Detail  string    `json:"detail"`
		Code    string    `json:"code"`
		Feature FeatureID `json:"feature"`
		Status  int       `json:"status"`
	}
)

const (
	FeatureVulnerabilityManagement FeatureID = "vulnerabilityManagement"

	FeatureVulnerabilityManagementSettingKey = "featureVulnerabilityManagementEnabled"

	FeatureSwarm FeatureID = "swarm"

	FeatureSwarmSettingKey = "featureSwarmEnabled"
)

var featureRegistry = [...]FeatureDefinition{
	{ID: FeatureVulnerabilityManagement, SettingKey: FeatureVulnerabilityManagementSettingKey, DefaultEnabled: true},
	{ID: FeatureSwarm, SettingKey: FeatureSwarmSettingKey, DefaultEnabled: false},
}

func AllFeatures() []FeatureDefinition {
	return append([]FeatureDefinition{}, featureRegistry[:]...)
}

func LookupFeature(id FeatureID) (FeatureDefinition, bool) {
	for _, definition := range featureRegistry {
		if definition.ID == id {
			return definition, true
		}
	}
	return FeatureDefinition{}, false
}

func (e *FeatureDisabledError) Error() string { return e.Detail }

func (e *FeatureDisabledError) GetStatus() int { return e.Status }

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
