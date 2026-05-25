import type { PageLoad } from './$types';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import { queryKeys } from '$lib/query/query-keys';
import { throwPageLoadError } from '$lib/utils/api';

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

	let environments;
	try {
		environments = await queryClient.fetchQuery({
			queryKey: queryKeys.environments.list(environmentRequestOptions),
			queryFn: () => environmentManagementService.getEnvironments(environmentRequestOptions)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load environments');
	}

	return { environments, environmentRequestOptions };
};
