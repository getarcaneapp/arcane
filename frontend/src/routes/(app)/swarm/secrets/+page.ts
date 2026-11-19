import { m } from '#lib/paraglide/messages.js';
import { swarmEnvironmentsQuery } from '#lib/query/swarm.js';
import { swarmKvListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { swarmKvResources } from '#lib/utils/swarm-kv.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const resource = swarmKvResources.secrets;
	const requestOptions = resolveInitialListPageRequest(resource.persistKey, {
		column: 'name',
		direction: 'asc'
	});

	const swarm = await tryCatch(queryClient.query(swarmEnvironmentsQuery('swarm-secrets:list')));
	if (swarm.error !== null) {
		throwPageLoadError(swarm.error, m.common_load_failed({ resource: resource.title() }));
	}
	const items = await tryCatch(queryClient.query(swarmKvListQuery(swarm.data, resource.kind, requestOptions)));
	if (items.error !== null) {
		throwPageLoadError(items.error, m.common_load_failed({ resource: resource.title() }));
	}

	return {
		swarm: swarm.data,
		items: items.data,
		requestOptions
	};
};
