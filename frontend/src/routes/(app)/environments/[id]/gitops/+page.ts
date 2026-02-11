import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();

	const environmentId = params.id;

	const syncRequestOptions = resolveInitialTableRequest('arcane-gitops-syncs-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const [environment, syncs] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.environments.detail(environmentId),
			queryFn: () => environmentManagementService.get(environmentId)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.gitOpsSyncs.list(environmentId, syncRequestOptions),
			queryFn: () => gitOpsSyncService.getSyncs(environmentId, syncRequestOptions)
		})
	]);

	return { environment, environmentId, syncs, syncRequestOptions };
};
