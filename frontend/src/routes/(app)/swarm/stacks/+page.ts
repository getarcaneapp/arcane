import { swarmService } from '#lib/services/swarm-service.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialListPageRequest('arcane-swarm-stacks-table', {
		column: 'name',
		direction: 'asc'
	});

	const stacks = await swarmService.getStacks(requestOptions);

	return {
		stacks,
		requestOptions
	};
};
