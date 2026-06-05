import { swarmService } from '$lib/services/swarm-service';
import { resolveInitialListPageRequest } from '$lib/utils/tables';
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
