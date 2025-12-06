import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
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

	const syncs = await gitOpsSyncService.getSyncs(syncRequestOptions);

	return { syncs, syncRequestOptions };
};
