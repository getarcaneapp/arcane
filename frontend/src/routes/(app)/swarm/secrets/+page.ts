import { m } from '#lib/paraglide/messages.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { swarmKvResources } from '#lib/utils/swarm-kv.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const resource = swarmKvResources.secrets;
	const requestOptions = resolveInitialListPageRequest(resource.persistKey, {
		column: 'name',
		direction: 'asc'
	});

	const items = await tryCatch(swarmService.getKvItems(resource.kind, requestOptions));
	if (items.error !== null) {
		throwPageLoadError(items.error, m.common_load_failed({ resource: resource.title() }));
	}

	return {
		items: items.data,
		requestOptions
	};
};
