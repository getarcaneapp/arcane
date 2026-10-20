import { swarmService } from '#lib/services/swarm-service.js';
import { swarmKvResources } from '#lib/utils/swarm-kv.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const resource = swarmKvResources.configs;
	const requestOptions = resolveInitialListPageRequest(resource.persistKey, {
		column: 'name',
		direction: 'asc'
	});

	return {
		items: await swarmService.getKvItems(resource.kind, requestOptions),
		requestOptions
	};
};
