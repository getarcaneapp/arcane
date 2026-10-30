import { s3DestinationListQuery } from '#lib/query/backups.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const requestOptions = resolveInitialTableRequest('arcane-s3-destinations-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'name', direction: 'asc' }
	} satisfies SearchPaginationSortRequest);
	const destinations = await queryClient.query(s3DestinationListQuery(requestOptions));
	return { destinations, requestOptions };
};
