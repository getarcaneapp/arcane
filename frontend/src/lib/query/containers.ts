import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { ContainerListRequestOptions } from '#lib/types/ui/container.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function containerListQuery(scope: string, environments: EnvironmentRef[], requestOptions: ContainerListRequestOptions) {
	return queryOptions({
		queryKey: queryKeys.containers.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				containerService.getContainersForEnvironment(id, request, config)
			)
	});
}

export function containerDetailQuery(environmentId: string, containerId: string) {
	return queryOptions({
		queryKey: queryKeys.containers.detail(environmentId, containerId),
		queryFn: () => containerService.getContainerForEnvironment(environmentId, containerId)
	});
}

export function containerEditConfigQuery(environmentId: string, containerId: string) {
	return queryOptions({
		queryKey: queryKeys.containers.editConfig(environmentId, containerId),
		queryFn: () => containerService.getContainerEditConfig(containerId, environmentId)
	});
}

export function containerProcessesQuery(environmentId: string, containerId: string) {
	return queryOptions({
		queryKey: queryKeys.containers.processes(environmentId, containerId),
		queryFn: ({ signal }) => containerService.getContainerProcesses(containerId, signal, environmentId)
	});
}
