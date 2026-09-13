import { tryCatch } from '#lib/utils/try-catch.js';
import { eventService } from '#lib/services/event-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const eventRequestOptions = resolveInitialTableRequest('arcane-events-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'timestamp',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

	let events;
	let eventStats;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.events.listGlobal(eventRequestOptions),
					queryFn: () => eventService.getEvents(eventRequestOptions)
				}),
				queryClient.query({
					queryKey: queryKeys.events.statsGlobal(),
					queryFn: () => eventService.getEventStats()
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load events');
	} else {
		[events, eventStats] = operationResult.data;
	}

	return { events, eventStats, eventRequestOptions };
};
