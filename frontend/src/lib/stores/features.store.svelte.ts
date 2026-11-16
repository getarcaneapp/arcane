import { QueryObserver, type QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { publicSettingsQuery } from '#lib/query/settings.js';
import { emptySettings, settingsService } from '#lib/services/settings-service.js';
import type { FeatureID } from '#lib/types/features.js';
import type { EnvironmentFeatures } from '#lib/types/ui/features.js';
import type { ResolvedSettings } from '#lib/types/ui/settings.js';
import { environmentName, limitEnvironments, sumAcrossEnvironments, type EnvironmentSum } from '#lib/utils/environments.js';
import { isFeatureEnabled, isSwarmFeatureEnabled, isVulnerabilityQuery, resolveFeatures } from '#lib/utils/features.js';

let environments = $state.raw<Record<string, EnvironmentFeatures>>({});
const unsubscribers = new Map<string, () => void>();
const appliedAt = new Map<string, number>();
let unsubscribeRefresh: (() => void) | undefined;
let queryClient: QueryClient | undefined;

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
	const observer = new QueryObserver(client, {
		...publicSettingsQuery(environmentId),
		refetchOnWindowFocus: false,
		refetchOnReconnect: false
	});
	unsubscribers.set(
		environmentId,
		observer.subscribe(() => void apply(environmentId))
	);
}

function watch(client: QueryClient) {
	unsubscribeRefresh ??= new QueryObserver(client, {
		queryKey: queryKeys.settings.publicAcross,
		queryFn: async () => {
			await fetchFeatures(client, [...unsubscribers.keys()], true);
			return null;
		},
		initialData: null,
		retry: false
	}).subscribe(() => undefined);
}

async function pick(aggregate: Promise<EnvironmentSum<ResolvedSettings>>, environmentId: string): Promise<ResolvedSettings> {
	const { byEnvironment, failedEnvironments } = await aggregate;
	const entry = byEnvironment.find(({ environment }) => environment.id === environmentId);
	if (entry) return entry.data;
	throw new Error(failedEnvironments.find(({ environment }) => environment.id === environmentId)?.message ?? environmentId);
}

async function fetchFeatures(client: QueryClient, environmentIds: string[], force: boolean): Promise<void> {
	if (environmentIds.length === 0) return;
	const { included, excluded } = limitEnvironments(environmentIds.map((id) => ({ id, name: environmentName(id) })));
	let aggregate: Promise<EnvironmentSum<ResolvedSettings>> | undefined;
	const shared = () =>
		(aggregate ??= sumAcrossEnvironments(
			included,
			(scope, config) => settingsService.getPublicSettingsForEnvironment(scope, config),
			emptySettings
		).finally(() => {
			aggregate = undefined;
		}));
	await Promise.all([
		...included.map(({ id }) =>
			client
				.query({ ...publicSettingsQuery(id), queryFn: () => pick(shared(), id), ...(force ? { staleTime: 0 } : {}) })
				.catch(() => undefined)
		),
		fetchFeatures(
			client,
			excluded.map(({ id }) => id),
			force
		)
	]);
}

async function load(environmentIds: string[], force = false): Promise<void> {
	const client = queryClient;
	if (!client) return;
	const pending = environmentIds.filter((id) => {
		const status = environments[id]?.status;
		return force || (status !== 'ready' && status !== 'unavailable');
	});
	if (pending.length === 0) return;
	const unknown = pending.filter((id) => !environments[id]?.status);
	if (unknown.length > 0) {
		environments = {
			...environments,
			...Object.fromEntries(unknown.map((id): [string, EnvironmentFeatures] => [id, { status: 'loading', features: {} }]))
		};
	}
	await fetchFeatures(client, pending, force);
	for (const id of pending) observe(client, id);
	watch(client);
	await Promise.all(pending.map((id) => apply(id)));
}

export const featureStore = {
	load,
	markUnavailable,
	refresh: (environmentIds: string[]) => load(environmentIds, true),
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
		unsubscribeRefresh?.();
		unsubscribeRefresh = undefined;
		queryClient?.removeQueries({ queryKey: queryKeys.settings.publicAcross, exact: true });
		appliedAt.clear();
		environments = {};
	}
};
