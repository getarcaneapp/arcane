import { variableService } from '$lib/services/variable-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import type { PageLoad } from './$types';

const environmentListOptions: SearchPaginationSortRequest = {
	pagination: { page: 1, limit: 1000 },
	sort: { column: 'name', direction: 'asc' }
};

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const [variables, environmentsPage] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.variables.list(),
			queryFn: () => variableService.list()
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.environments.list(environmentListOptions),
			queryFn: () => environmentManagementService.getEnvironments(environmentListOptions)
		})
	]);

	return { variables, environments: environmentsPage.data };
};
