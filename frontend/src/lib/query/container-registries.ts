import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerRegistryService } from '#lib/services/container-registry-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

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
