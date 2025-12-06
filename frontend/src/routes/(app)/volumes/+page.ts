import { volumeService } from '$lib/services/volume-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const volumeRequestOptions = resolveInitialTableRequest('arcane-volumes-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	// Single API call - counts are included in the response
	const volumes = await volumeService.getVolumes(volumeRequestOptions);

	return {
		volumes,
		volumeRequestOptions,
		// Use counts from the volumes response
		volumeUsageCounts: volumes.counts ?? { inuse: 0, unused: 0, total: 0 }
	};
};
