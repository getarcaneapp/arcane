import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { hasPermission } from '#lib/utils/auth.js';
import { readableEnvironments, sumAcrossEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';

export type SwarmEnvironments = {
	environments: EnvironmentRef[];
	managers: EnvironmentRef[];
	clusterIds: Record<string, string>;
	failedEnvironments: EnvironmentFailure[];
	scope: string;
};

export function onePerCluster(managers: EnvironmentRef[], clusterIds: Record<string, string>): EnvironmentRef[] {
	const clusters = new Set<string>();
	return managers.filter((environment) => {
		const clusterId = clusterIds[environment.id] ?? '';
		if (clusters.has(clusterId)) return false;
		clusters.add(clusterId);
		return true;
	});
}

function swarmReadableEnvironments(permission: string): EnvironmentRef[] {
	return readableEnvironments(permission).filter((environment) => hasPermission('swarm:read', environment.id));
}

export async function loadSwarmEnvironments(permission: string): Promise<SwarmEnvironments> {
	await environmentStore.ready;
	const readable = swarmReadableEnvironments(permission);
	const status = await sumAcrossEnvironments(readable, (scope, config) => swarmService.getSwarmStatus(scope, config), {
		enabled: false
	});
	const clusterIds = Object.fromEntries(status.byEnvironment.map((entry) => [entry.environment.id, entry.data.clusterId ?? '']));
	const managers = readable.filter((environment) => clusterIds[environment.id]);
	const environments = onePerCluster(managers, clusterIds);
	return {
		environments,
		managers,
		clusterIds,
		failedEnvironments: status.failedEnvironments,
		scope: environments.map((environment) => environment.id).join(',')
	};
}

export function swarmEnvironmentsQueryOptions(permission: string) {
	return {
		queryKey: queryKeys.swarm.environments(
			swarmReadableEnvironments(permission)
				.map((environment) => environment.id)
				.join(','),
			permission
		),
		queryFn: () => loadSwarmEnvironments(permission)
	};
}
