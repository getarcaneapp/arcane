import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { templateService } from '#lib/services/template-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function templateListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.templates.list(requestOptions),
		queryFn: () => templateService.getTemplates(requestOptions)
	});
}

export function templateRegistriesQuery() {
	return queryOptions({
		queryKey: queryKeys.templates.registries(),
		queryFn: () => templateService.getRegistries()
	});
}
