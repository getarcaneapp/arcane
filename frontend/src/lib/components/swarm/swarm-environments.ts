import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { readableEnvironments, sumAcrossEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';

export type SwarmEnvironments = {
	environments: EnvironmentRef[];
	managers: EnvironmentRef[];
	failedEnvironments: EnvironmentFailure[];
	scope: string;
};

export async function loadSwarmEnvironments(permission: string): Promise<SwarmEnvironments> {
	await environmentStore.ready;
	const readable = readableEnvironments(permission);
	const status = await sumAcrossEnvironments(readable, (scope, config) => swarmService.getSwarmStatus(scope, config), {
		enabled: false
	});
	const clusterIds = new Map(status.byEnvironment.map((entry) => [entry.environment.id, entry.data.clusterId]));
	const managers = readable.filter((environment) => clusterIds.get(environment.id));
	const clusters = new Set<string>();
	const environments = managers.filter((environment) => {
		const clusterId = clusterIds.get(environment.id) ?? '';
		if (clusters.has(clusterId)) return false;
		clusters.add(clusterId);
		return true;
	});
	return {
		environments,
		managers,
		failedEnvironments: status.failedEnvironments,
		scope: environments.map((environment) => environment.id).join(',')
	};
}

export function swarmEnvironmentsQueryOptions(permission: string) {
	return {
		queryKey: queryKeys.swarm.environments(
			readableEnvironments(permission)
				.map((environment) => environment.id)
				.join(','),
			permission
		),
		queryFn: () => loadSwarmEnvironments(permission)
	};
}
