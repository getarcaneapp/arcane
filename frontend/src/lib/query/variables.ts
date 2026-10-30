import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { variableService } from '#lib/services/variable-service.js';

export function variableListQuery() {
	return queryOptions({
		queryKey: queryKeys.variables.list(),
		queryFn: () => variableService.list()
	});
}
