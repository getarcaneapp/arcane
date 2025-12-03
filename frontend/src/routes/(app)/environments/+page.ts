import type { PageLoad } from './$types';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';

export const load: PageLoad = async () => {
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

	const environments = await environmentManagementService.getEnvironments(environmentRequestOptions);

	return { environments, environmentRequestOptions };
};
