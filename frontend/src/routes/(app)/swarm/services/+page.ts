import { swarmService } from '$lib/services/swarm-service';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-swarm-services-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const services = await swarmService.getServices(requestOptions);

	return {
		services,
		requestOptions
	};
};
