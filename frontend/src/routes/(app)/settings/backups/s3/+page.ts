import type { PageLoad } from './$types';
import { s3DestinationService } from '#lib/services/s3-destination-service.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-s3-destinations-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'name', direction: 'asc' }
	} satisfies SearchPaginationSortRequest);
	const destinations = await s3DestinationService.list(requestOptions);
	return { destinations, requestOptions };
};
