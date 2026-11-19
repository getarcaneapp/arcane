import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { diagnosticsService } from '#lib/services/diagnostics-service.js';

export function goroutineProfileQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.diagnostics.goroutines(environmentId),
		queryFn: () => diagnosticsService.getGoroutinesForEnvironment(environmentId)
	});
}
