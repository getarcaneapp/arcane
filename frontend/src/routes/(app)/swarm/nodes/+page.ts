import { swarmService } from '#lib/services/swarm-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-swarm-nodes-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'hostname',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const nodes = await swarmService.getNodes(requestOptions);

	return {
		nodes,
		requestOptions
	};
};
