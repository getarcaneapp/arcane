export type FeatureID = 'vulnerabilityManagement' | 'swarm';

export type FeatureDefinition = {
	id: FeatureID;
	settingKey: 'featureVulnerabilityManagementEnabled' | 'featureSwarmEnabled';
	defaultEnabled: boolean;
};
