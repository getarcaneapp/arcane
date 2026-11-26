import { groupListQuery } from '#lib/query/groups.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const groupsRequestOptions = resolveInitialTableRequest('arcane-groups-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const groups = await queryClient.query(groupListQuery(groupsRequestOptions));

	return {
		groups,
		groupsRequestOptions
	};
};
