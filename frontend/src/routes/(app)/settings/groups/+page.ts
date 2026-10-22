import { queryKeys } from '#lib/query/query-keys.js';
import { groupService } from '#lib/services/group-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

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

	const result = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.groups.list(groupsRequestOptions),
			queryFn: () => groupService.getGroups(groupsRequestOptions)
		})
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, 'Failed to load groups');
	}

	return {
		groups: result.data,
		groupsRequestOptions
	};
};
