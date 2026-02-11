import { eventService } from '$lib/services/event-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

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

	const events = await queryClient.fetchQuery({
		queryKey: queryKeys.events.listByEnvironment(envId, eventRequestOptions),
		queryFn: () => eventService.getEventsForEnvironment(envId, eventRequestOptions)
	});

	return { events, eventRequestOptions };
};
