import { queryOptions } from '@tanstack/svelte-query';

import { fetchEnvironmentTopology } from '#lib/components/network-diagram/topology.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { networkService } from '#lib/services/network-service.js';
import { portService } from '#lib/services/port-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function networkListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.networks.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				networkService.getNetworksForEnvironment(id, request, config)
			)
	});
}

export function portListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.ports.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				portService.getPortsForEnvironment(id, request, config)
			)
	});
}

export function networkListForEnvironmentQuery(environmentId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.networks.list(environmentId, requestOptions),
		queryFn: () => networkService.getNetworksForEnvironment(environmentId, requestOptions)
	});
}

export function networkDetailQuery(environmentId: string, networkId: string) {
	return queryOptions({
		queryKey: queryKeys.networks.detail(environmentId, networkId),
		queryFn: () => networkService.getNetworkForEnvironment(environmentId, networkId)
	});
}

export function networkTopologyQuery(scope: string) {
	return queryOptions({
		queryKey: queryKeys.networks.topology(scope),
		queryFn: () => fetchEnvironmentTopology()
	});
}
