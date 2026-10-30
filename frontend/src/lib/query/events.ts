import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { eventService } from '#lib/services/event-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function eventListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.events.listGlobal(requestOptions),
		queryFn: () => eventService.getEvents(requestOptions)
	});
}

export function eventStatsQuery() {
	return queryOptions({
		queryKey: queryKeys.events.statsGlobal(),
		queryFn: () => eventService.getEventStats()
	});
}
