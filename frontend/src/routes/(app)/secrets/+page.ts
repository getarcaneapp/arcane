import { environmentListQuery } from '#lib/query/environments.js';
import { secretSourceListQuery } from '#lib/query/secret-sources.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

const environmentListOptions: SearchPaginationSortRequest = {
	pagination: { page: 1, limit: 1000 },
	sort: { column: 'name', direction: 'asc' }
};

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const result = await tryCatch(
		Promise.all([queryClient.query(secretSourceListQuery()), queryClient.query(environmentListQuery(environmentListOptions))])
	);
	if (result.error !== null) throwPageLoadError(result.error, 'Failed to load secret sources');

	const [sources, environmentsPage] = result.data;
	return { sources, environments: environmentsPage.data };
};
