import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
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
