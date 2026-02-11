import type { PageLoad } from './$types';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import { queryKeys } from '$lib/query/query-keys';

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

	const environments = await queryClient.fetchQuery({
		queryKey: queryKeys.environments.list(environmentRequestOptions),
		queryFn: () => environmentManagementService.getEnvironments(environmentRequestOptions)
	});

	return { environments, environmentRequestOptions };
};
