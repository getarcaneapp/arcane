import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { apiKeyService } from '#lib/services/api-key-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function apiKeyListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.apiKeys.list(requestOptions),
		queryFn: () => apiKeyService.getApiKeys(requestOptions)
	});
}
