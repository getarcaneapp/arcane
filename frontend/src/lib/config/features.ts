import type { FeatureDefinition } from '#lib/types/features.js';

export const featureDefinitions = [
	{
		id: 'vulnerabilityManagement',
		settingKey: 'featureVulnerabilityManagementEnabled',
		defaultEnabled: true
	}
] as const satisfies readonly FeatureDefinition[];
