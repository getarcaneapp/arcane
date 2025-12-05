import { networkService } from '$lib/services/network-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const networkRequestOptions = resolveInitialTableRequest('arcane-networks-table', {
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
	const networks = await networkService.getNetworks(networkRequestOptions);

	return {
		networks,
		networkRequestOptions,
		// Use counts from the networks response
		networkUsageCounts: networks.counts ?? { inuse: 0, unused: 0, total: 0 }
	};
};
