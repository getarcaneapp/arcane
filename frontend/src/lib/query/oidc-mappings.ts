import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { oidcMappingService } from '#lib/services/oidc-mapping-service.js';

export function oidcMappingListQuery() {
	return queryOptions({
		queryKey: queryKeys.oidcMappings.list(),
		queryFn: () => oidcMappingService.list()
	});
}
