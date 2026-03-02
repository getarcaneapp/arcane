import { eventService } from '$lib/services/event-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
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
	try {
		events = await queryClient.fetchQuery({
			queryKey: queryKeys.events.listGlobal(eventRequestOptions),
			queryFn: () => eventService.getEvents(eventRequestOptions)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load events');
	}

	return { events, eventRequestOptions };
};
