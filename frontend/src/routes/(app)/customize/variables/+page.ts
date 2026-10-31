import { environmentListQuery } from '#lib/query/environments.js';
import { variableListQuery } from '#lib/query/variables.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

import type { PageLoad } from './$types';

const environmentListOptions: SearchPaginationSortRequest = {
	pagination: { page: 1, limit: 1000 },
	sort: { column: 'name', direction: 'asc' }
};

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const [variables, environmentsPage] = await Promise.all([
		queryClient.query(variableListQuery()),
		queryClient.query(environmentListQuery(environmentListOptions))
	]);

	return { variables, environments: environmentsPage.data };
};
