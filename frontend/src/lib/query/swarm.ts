import { queryOptions } from '@tanstack/svelte-query';

import type { SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import type { SwarmKvKind } from '#lib/types/swarm.js';
import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';

export function swarmStacksScope(swarm: SwarmEnvironments) {
	return swarm.managers.map((env) => env.id).join(',');
}

export function swarmNodeListQuery(swarm: SwarmEnvironments, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.swarm.nodes(swarm.scope, requestOptions),
		queryFn: async () =>
			withFailedEnvironments(
				await fetchAcrossEnvironments(swarm.environments, requestOptions, (id, request, config) =>
					swarmService.getNodes(request, id, config)
				),
				swarm.failedEnvironments
			)
	});
}

export function swarmServiceListQuery(swarm: SwarmEnvironments, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.swarm.services(swarm.scope, requestOptions),
		queryFn: async () =>
			withFailedEnvironments(
				await fetchAcrossEnvironments(swarm.environments, requestOptions, (id, request, config) =>
					swarmService.getServices(request, id, config)
				),
				swarm.failedEnvironments
			)
	});
}

export function swarmTaskListQuery(swarm: SwarmEnvironments, nodeId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.swarm.tasks(swarm.scope, nodeId, requestOptions),
		queryFn: async () =>
			withFailedEnvironments(
				await fetchAcrossEnvironments(swarm.environments, requestOptions, (id, request, config) => {
					if (nodeId) return swarmService.getNodeTasks(nodeId, request, id, config);
					return swarmService.getTasks(request, id, config);
				}),
				swarm.failedEnvironments
			)
	});
}

export function swarmStackListQuery(swarm: SwarmEnvironments, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.swarm.stacks(swarmStacksScope(swarm), requestOptions),
		queryFn: async () =>
			withFailedEnvironments(
				await fetchAcrossEnvironments(swarm.managers, requestOptions, (id, request, config) =>
					swarmService.getStacks(request, id, config)
				),
				swarm.failedEnvironments
			)
	});
}

export function swarmKvListQuery(swarm: SwarmEnvironments, kind: SwarmKvKind, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.swarm[kind](swarm.scope, requestOptions),
		queryFn: async () =>
			withFailedEnvironments(
				await fetchAcrossEnvironments(swarm.environments, requestOptions, (id, request, config) =>
					swarmService.getKvItems(kind, request, id, config)
				),
				swarm.failedEnvironments
			)
	});
}

export function swarmStackQuery(environmentId: string, name: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.stack(environmentId, name),
		queryFn: () => swarmService.getStack(name, environmentId)
	});
}

export function swarmStackServiceListQuery(
	environment: EnvironmentRef,
	name: string,
	requestOptions: SearchPaginationSortRequest
) {
	return queryOptions({
		queryKey: queryKeys.swarm.stackServices(environment.id, name, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments([environment], requestOptions, (id, request, config) =>
				swarmService.getStackServices(name, request, id, config)
			)
	});
}

export function swarmStackTaskListQuery(environment: EnvironmentRef, name: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.swarm.stackTasks(environment.id, name, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments([environment], requestOptions, (id, request, config) =>
				swarmService.getStackTasks(name, request, id, config)
			)
	});
}
