import type { FeatureID } from '../features';

export type FeatureState = { enabled: boolean; supported: boolean };

export type EnvironmentFeatures = {
	status: 'loading' | 'ready' | 'unavailable';
	features: Partial<Record<FeatureID, FeatureState>>;
};
