import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params }) => {
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
		environmentManagementService.get(environmentId),
		gitOpsSyncService.getSyncs(environmentId, syncRequestOptions)
	]);

	return { environment, environmentId, syncs, syncRequestOptions };
};
