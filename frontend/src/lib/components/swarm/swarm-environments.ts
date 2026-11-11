import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { environmentStatusStore } from '#lib/stores/environment-status.store.svelte.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { hasPermission } from '#lib/utils/auth.js';
import { readableEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';

export type SwarmEnvironments = {
	environments: EnvironmentRef[];
	managers: EnvironmentRef[];
	failedEnvironments: EnvironmentFailure[];
	scope: string;
};

function swarmReadableEnvironments(permission: string): EnvironmentRef[] {
	return readableEnvironments(permission).filter((environment) => hasPermission('swarm:read', environment.id));
}

function swarmStatusKey(environment: EnvironmentRef): string {
	const status = environmentStatusStore.swarmStatus(environment.id);
	return status ? (status.clusterId ?? '') : '-';
}

export async function loadSwarmEnvironments(permission: string): Promise<SwarmEnvironments> {
	await Promise.all([environmentStore.ready, environmentStatusStore.runtimeReady()]);
	const readable = swarmReadableEnvironments(permission);
	const managers = readable.filter((environment) => environmentStatusStore.swarmStatus(environment.id)?.clusterId);
	const clusters = new Set<string>();
	const environments = managers.filter((environment) => {
		const clusterId = environmentStatusStore.swarmStatus(environment.id)?.clusterId ?? '';
		if (clusters.has(clusterId)) return false;
		clusters.add(clusterId);
		return true;
	});
	return {
		environments,
		managers,
		failedEnvironments: environmentStatusStore.runtimeReceived
			? readable.flatMap((environment) =>
					environmentStatusStore.swarmStatus(environment.id) ? [] : [{ environment, message: m.common_unavailable() }]
				)
			: [],
		scope: environments.map((environment) => environment.id).join(',')
	};
}

export function swarmEnvironmentsQueryOptions(permission: string) {
	const readable = swarmReadableEnvironments(permission);
	return {
		queryKey: queryKeys.swarm.environments(
			readable.map((environment) => environment.id).join(','),
			permission,
			readable.map(swarmStatusKey).join(',')
		),
		queryFn: () => loadSwarmEnvironments(permission)
	};
}
