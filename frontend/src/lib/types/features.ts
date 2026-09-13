export type FeatureID = 'vulnerabilityManagement';

export type FeatureDefinition = {
	id: FeatureID;
	settingKey: 'featureVulnerabilityManagementEnabled';
	defaultEnabled: boolean;
};

export type FeatureState = { enabled: boolean; supported: boolean };
export type EnvironmentFeatures = {
	status: 'loading' | 'ready' | 'unavailable';
	features: Partial<Record<FeatureID, FeatureState>>;
};
