import type { FeatureDefinition } from '#lib/types/features.js';

export const featureDefinitions = [
	{
		id: 'vulnerabilityManagement',
		settingKey: 'featureVulnerabilityManagementEnabled',
		defaultEnabled: true
	},
	{
		id: 'swarm',
		settingKey: 'featureSwarmEnabled',
		defaultEnabled: false
	}
] as const satisfies readonly FeatureDefinition[];
