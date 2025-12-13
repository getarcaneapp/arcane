import { eventService } from '$lib/services/event-service';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
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

	let environmentId = LOCAL_DOCKER_ENVIRONMENT_ID;
	try {
		environmentId = await environmentStore.getCurrentEnvironmentId();
	} catch {
		// Fallback to local environment when store isn't ready
	}

	const events = await eventService.getEventsForEnvironment(environmentId, eventRequestOptions);

	return { events, eventRequestOptions };
};
