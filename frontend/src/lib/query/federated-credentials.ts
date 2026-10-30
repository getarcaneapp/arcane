import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { federatedCredentialService } from '#lib/services/federated-credential-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function federatedCredentialListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.federatedCredentials.list(requestOptions),
		queryFn: () => federatedCredentialService.list(requestOptions)
	});
}
