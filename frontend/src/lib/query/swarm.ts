import { queryOptions } from '@tanstack/svelte-query';

import {
	loadSwarmEnvironments,
	swarmReadableEnvironments,
	type SwarmEnvironments
} from '#lib/components/swarm/swarm-environments.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { SwarmKvKind } from '#lib/types/ui/swarm.js';
import type { User } from '#lib/types/user.js';
import {
	fetchAcrossEnvironments,
	readEachEnvironment,
	sumAcrossEnvironments,
	withFailedEnvironments
} from '#lib/utils/environments.js';

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

export function swarmServiceQuery(environmentId: string, serviceId: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.service(environmentId, serviceId),
		queryFn: () => swarmService.getService(serviceId, environmentId)
	});
}

export function swarmStackSourceQuery(environmentId: string, name: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.stackSource(environmentId, name),
		queryFn: () => swarmService.getStackSource(name, environmentId)
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

export function swarmEnvironmentsQuery(permission: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.environments(
			swarmReadableEnvironments(permission)
				.map((environment) => environment.id)
				.join(','),
			permission
		),
		queryFn: () => loadSwarmEnvironments(permission)
	});
}

export function swarmStatusQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.swarm.status(environments.map((environment) => environment.id).join(',')),
		queryFn: () =>
			sumAcrossEnvironments(environments, (scope, config) => swarmService.getSwarmStatus(scope, config), {
				enabled: false
			})
	});
}

export function swarmInfoQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.info(environmentId),
		queryFn: () => swarmService.getSwarmInfo(environmentId)
	});
}

export function swarmJoinTokensQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.joinTokens(environmentId),
		queryFn: () => swarmService.getSwarmJoinTokens(environmentId)
	});
}

export function swarmUnlockKeyQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.unlockKey(environmentId),
		queryFn: () => swarmService.getSwarmUnlockKey(environmentId)
	});
}

export function swarmJoinCandidatesQuery(environmentId: string | undefined, user: Pick<User, 'id' | 'permissionsByEnv'> | null) {
	return queryOptions({
		queryKey: queryKeys.swarm.joinCandidates(environmentId ?? null, user),
		queryFn: () => swarmService.getSwarmJoinCandidates(environmentId ?? '')
	});
}

export function swarmJoinCandidateManagersQuery(
	environments: EnvironmentRef[],
	user: Pick<User, 'id' | 'permissionsByEnv'> | null
) {
	return queryOptions({
		queryKey: queryKeys.swarm.joinCandidates(environments.map((env) => env.id).join(','), user),
		queryFn: async ({ signal }): Promise<Record<string, string>> => {
			const { byEnvironment } = await readEachEnvironment(
				environments,
				(scope, config) => swarmService.getSwarmJoinCandidates(scope, { ...config, signal }),
				[]
			);
			signal.throwIfAborted();
			return Object.fromEntries(
				byEnvironment.flatMap(({ environment, data }) =>
					data.map((candidate) => [candidate.environmentId, environment.id] as const)
				)
			);
		}
	});
}

export function swarmServiceTasksQuery(environmentId: string, serviceId: string) {
	return queryOptions({
		queryKey: queryKeys.swarm.serviceTasks(environmentId, serviceId),
		queryFn: () => swarmService.getServiceTasks(serviceId, { pagination: { page: 1, limit: 100 } }, environmentId)
	});
}
