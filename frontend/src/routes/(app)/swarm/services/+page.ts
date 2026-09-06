import { swarmService } from '#lib/services/swarm-service.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialListPageRequest('arcane-swarm-services-table', {
		column: 'name',
		direction: 'asc'
	});

	const services = await swarmService.getServices(requestOptions);

	return {
		services,
		requestOptions
	};
};
