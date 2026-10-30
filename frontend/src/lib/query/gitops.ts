import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function gitOpsSyncListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				gitOpsSyncService.getSyncs(id, request, config)
			)
	});
}
