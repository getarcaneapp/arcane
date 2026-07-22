import { eventService } from '$lib/services/event-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import { throwPageLoadError } from '$lib/utils/api';
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
	try {
		[events, eventStats] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.events.listGlobal(eventRequestOptions),
				queryFn: () => eventService.getEvents(eventRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.events.statsGlobal(),
				queryFn: () => eventService.getEventStats()
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load events');
	}

	return { events, eventStats, eventRequestOptions };
};
