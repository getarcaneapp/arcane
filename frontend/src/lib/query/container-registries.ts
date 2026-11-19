import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerRegistryService } from '#lib/services/container-registry-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';

export function containerRegistryListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.containerRegistries.list(requestOptions),
		queryFn: () => containerRegistryService.getRegistries(requestOptions)
	});
}

export function containerRegistryPullUsageQuery() {
	return queryOptions({
		queryKey: queryKeys.containerRegistries.pullUsage(),
		queryFn: () => containerRegistryService.getPullUsage()
	});
}

export function containerRegistryDetailQuery(registryId: string) {
	return queryOptions({
		queryKey: queryKeys.containerRegistries.detail(registryId),
		queryFn: () => containerRegistryService.getRegistry(registryId)
	});
}

export function containerRegistryRepositoriesQuery(registryId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.containerRegistries.repositories(registryId, requestOptions),
		queryFn: () => containerRegistryService.getRepositories(registryId, requestOptions)
	});
}

export function containerRegistryTagsQuery(registryId: string, repository: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.containerRegistries.tags(registryId, repository, requestOptions),
		queryFn: () => containerRegistryService.getTags(registryId, repository, requestOptions)
	});
}
