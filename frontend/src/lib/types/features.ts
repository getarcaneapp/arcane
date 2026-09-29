export type FeatureID = 'vulnerabilityManagement' | 'swarm';

export type FeatureDefinition = {
	id: FeatureID;
	settingKey: 'featureVulnerabilityManagementEnabled' | 'featureSwarmEnabled';
	defaultEnabled: boolean;
};

export type FeatureState = { enabled: boolean; supported: boolean };
export type EnvironmentFeatures = {
	status: 'loading' | 'ready' | 'unavailable';
	features: Partial<Record<FeatureID, FeatureState>>;
};
