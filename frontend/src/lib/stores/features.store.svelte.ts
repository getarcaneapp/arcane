import { QueryObserver, type QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import type { EnvironmentFeatures, FeatureID } from '#lib/types/features.js';
import type { ResolvedSettings } from '#lib/types/settings.js';
import { isFeatureEnabled, isSwarmFeatureEnabled, isVulnerabilityQuery, resolveFeatures } from '#lib/utils/features.js';

let environments = $state.raw<Record<string, EnvironmentFeatures>>({});
const unsubscribers = new Map<string, () => void>();
const appliedAt = new Map<string, number>();
let queryClient: QueryClient | undefined;

function featureQuery(environmentId: string) {
	return {
		queryKey: queryKeys.settings.public(environmentId),
		queryFn: ({ signal }: { signal: AbortSignal }) => settingsService.getPublicSettingsForEnvironment(environmentId, signal),
		retry: false,
		retryOnMount: false
	};
}

async function clearVulnerabilityQueries(environmentId: string): Promise<void> {
	if (!queryClient) return;
	const filters = { predicate: (query: { queryKey: readonly unknown[] }) => isVulnerabilityQuery(query.queryKey, environmentId) };
	await queryClient.cancelQueries(filters);
	queryClient.removeQueries(filters);
}

async function setUnavailable(environmentId: string): Promise<void> {
	environments = { ...environments, [environmentId]: { status: 'unavailable', features: {} } };
	await clearVulnerabilityQueries(environmentId);
}

async function markUnavailable(environmentId: string): Promise<void> {
	await queryClient?.cancelQueries({ queryKey: queryKeys.settings.public(environmentId), exact: true });
	await setUnavailable(environmentId);
}

async function apply(environmentId: string): Promise<void> {
	const state = queryClient?.getQueryState<ResolvedSettings>(queryKeys.settings.public(environmentId));
	const updatedAt = Math.max(state?.dataUpdatedAt ?? 0, state?.errorUpdatedAt ?? 0);
	if (!state || updatedAt === 0 || appliedAt.get(environmentId) === updatedAt) return;
	appliedAt.set(environmentId, updatedAt);
	if (state.status === 'error' || !state.data) {
		await setUnavailable(environmentId);
		return;
	}
	const features = resolveFeatures(state.data.values);
	environments = { ...environments, [environmentId]: features };
	if (!isFeatureEnabled(features, 'vulnerabilityManagement')) await clearVulnerabilityQueries(environmentId);
}

function observe(client: QueryClient, environmentId: string) {
	if (unsubscribers.has(environmentId)) return;
	const observer = new QueryObserver(client, featureQuery(environmentId));
	unsubscribers.set(
		environmentId,
		observer.subscribe(() => void apply(environmentId))
	);
}

async function load(environmentId: string, force = false): Promise<void> {
	const status = environments[environmentId]?.status;
	if (!force && (status === 'ready' || status === 'unavailable')) return;
	if (!queryClient) return;
	if (!status) {
		environments = { ...environments, [environmentId]: { status: 'loading', features: {} } };
	}
	await queryClient.query({ ...featureQuery(environmentId), ...(force ? { staleTime: 0 } : {}) }).catch(() => undefined);
	observe(queryClient, environmentId);
	await apply(environmentId);
}

export const featureStore = {
	load,
	markUnavailable,
	refresh: (environmentId: string) => load(environmentId, true),
	status: (environmentId: string) => environments[environmentId]?.status ?? 'loading',
	isEnabled: (id: FeatureID, environmentId: string) => isFeatureEnabled(environments[environmentId], id),
	isSwarmEnabled: (environmentId: string, swarmActive: boolean) =>
		isSwarmFeatureEnabled(environments[environmentId], swarmActive),
	isSupported: (id: FeatureID, environmentId: string) =>
		environments[environmentId]?.status === 'ready' && environments[environmentId]?.features[id]?.supported === true,
	connect(client: QueryClient) {
		queryClient = client;
	},
	clear() {
		for (const unsubscribe of unsubscribers.values()) unsubscribe();
		unsubscribers.clear();
		appliedAt.clear();
		environments = {};
	}
};
