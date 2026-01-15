import { swarmService } from '$lib/services/swarm-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-swarm-tasks-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'service',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const tasks = await swarmService.getTasks(requestOptions);

	return {
		tasks,
		requestOptions
	};
};
