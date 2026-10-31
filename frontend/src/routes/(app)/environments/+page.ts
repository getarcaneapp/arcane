import { environmentListQuery } from '#lib/query/environments.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const environmentRequestOptions = resolveInitialTableRequest('arcane-environments-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'timestamp',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

	const environments = await queryClient
		.query(environmentListQuery(environmentRequestOptions))
		.catch((err: unknown) => throwPageLoadError(err, 'Failed to load environments'));

	return { environments, environmentRequestOptions };
};
