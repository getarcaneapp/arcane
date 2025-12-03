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

	const [networks, networkUsageCounts] = await Promise.all([
		networkService.getNetworks(networkRequestOptions),
		networkService.getNetworkUsageCounts()
	]);

	return { networks, networkRequestOptions, networkUsageCounts };
};
