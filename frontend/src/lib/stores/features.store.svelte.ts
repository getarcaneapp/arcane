import type { QueryClient } from '@tanstack/svelte-query';
import { settingsService } from '#lib/services/settings-service.js';
import type { EnvironmentFeatures, FeatureID } from '#lib/types/features.js';
import { isFeatureEnabled, isVulnerabilityQuery, resolveFeatures } from '#lib/utils/features.js';

let environments = $state.raw<Record<string, EnvironmentFeatures>>({});
const requests = new Map<string, Promise<void>>();
const controllers = new Map<string, AbortController>();
let queryClient: QueryClient | undefined;

async function clearVulnerabilityQueries(environmentId: string): Promise<void> {
	if (!queryClient) return;
	const filters = { predicate: (query: { queryKey: readonly unknown[] }) => isVulnerabilityQuery(query.queryKey, environmentId) };
	await queryClient.cancelQueries(filters);
	queryClient.removeQueries(filters);
}

async function markUnavailable(environmentId: string): Promise<void> {
	controllers.get(environmentId)?.abort();
	controllers.delete(environmentId);
	requests.delete(environmentId);
	environments = { ...environments, [environmentId]: { status: 'unavailable', features: {} } };
	await clearVulnerabilityQueries(environmentId);
}

async function load(environmentId: string, force = false): Promise<void> {
	if (!force) {
		const pending = requests.get(environmentId);
		if (pending) return pending;
		if (environments[environmentId]?.status === 'ready') return;
	}
	controllers.get(environmentId)?.abort();
	const controller = new AbortController();
	controllers.set(environmentId, controller);
	if (!environments[environmentId]) {
		environments = { ...environments, [environmentId]: { status: 'loading', features: {} } };
	}
	const request = (async () => {
		try {
			const settings = await settingsService.getPublicSettingsForEnvironment(environmentId, controller.signal);
			if (controller.signal.aborted) return;
			const state = resolveFeatures(settings);
			environments = { ...environments, [environmentId]: state };
			if (!isFeatureEnabled(state, 'vulnerabilityManagement')) await clearVulnerabilityQueries(environmentId);
		} catch {
			if (controller.signal.aborted) return;
			await markUnavailable(environmentId);
		} finally {
			if (controllers.get(environmentId) === controller) {
				requests.delete(environmentId);
				controllers.delete(environmentId);
			}
		}
	})();
	requests.set(environmentId, request);
	await request;
	while (controller.signal.aborted) {
		const replacement = requests.get(environmentId);
		if (!replacement || replacement === request) break;
		await replacement;
	}
}

export const featureStore = {
	load,
	markUnavailable,
	refresh: (environmentId: string) => load(environmentId, true),
	refreshKnown: () => Promise.all(Object.keys(environments).map((id) => load(id, true))),
	status: (environmentId: string) => environments[environmentId]?.status ?? 'loading',
	isEnabled: (id: FeatureID, environmentId: string) => isFeatureEnabled(environments[environmentId], id),
	isSupported: (id: FeatureID, environmentId: string) =>
		environments[environmentId]?.status === 'ready' && environments[environmentId]?.features[id]?.supported === true,
	connect(client: QueryClient) {
		queryClient = client;
	},
	clear() {
		for (const controller of controllers.values()) controller.abort();
		controllers.clear();
		requests.clear();
		environments = {};
	}
};
