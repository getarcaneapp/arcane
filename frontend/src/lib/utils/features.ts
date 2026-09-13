import { featureDefinitions } from '#lib/config/features.js';
import type { EnvironmentFeatures, FeatureID } from '#lib/types/features.js';
import type { Settings } from '#lib/types/settings.js';

export function resolveFeatures(settings: Partial<Settings>): EnvironmentFeatures {
	const features: EnvironmentFeatures['features'] = {};
	for (const feature of featureDefinitions) {
		const value = settings[feature.settingKey];
		features[feature.id] = {
			enabled: typeof value === 'boolean' ? value : feature.defaultEnabled,
			supported: typeof value === 'boolean'
		};
	}
	return { status: 'ready', features };
}

export function isFeatureEnabled(state: EnvironmentFeatures | undefined, id: FeatureID): boolean {
	return state?.status === 'ready' && state.features[id]?.enabled === true;
}

export function isVulnerabilityQuery(queryKey: readonly unknown[], environmentId: string): boolean {
	return queryKey[0] === 'vulnerabilities' && queryKey[2] === environmentId;
}
