import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function podmanPodListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.podman.pods(scope, requestOptions, environments.map((env) => env.id).join(',')),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) => podmanService.getPods(id, request, config))
	});
}

export function podmanQuadletListQuery(
	scope: string,
	environments: EnvironmentRef[],
	requestOptions: SearchPaginationSortRequest
) {
	return queryOptions({
		queryKey: queryKeys.podman.quadlets(scope, requestOptions, environments.map((env) => env.id).join(',')),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				podmanService.getQuadlets(id, request, config)
			)
	});
}

export function podmanSecretListQuery(
	scope: string,
	environments: EnvironmentRef[],
	requestOptions: SearchPaginationSortRequest
) {
	return queryOptions({
		queryKey: queryKeys.podman.secrets(scope, requestOptions, environments.map((env) => env.id).join(',')),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				podmanService.getSecrets(id, request, config)
			)
	});
}
