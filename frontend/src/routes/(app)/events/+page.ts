import { eventListQuery, eventStatsQuery } from '#lib/query/events.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

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
		(async () => Promise.all([queryClient.query(eventListQuery(eventRequestOptions)), queryClient.query(eventStatsQuery())]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load events');
	} else {
		[events, eventStats] = operationResult.data;
	}

	return { events, eventStats, eventRequestOptions };
};
