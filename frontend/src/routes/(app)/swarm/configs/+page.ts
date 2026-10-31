import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { swarmKvListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { swarmKvResources } from '#lib/utils/swarm-kv.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const resource = swarmKvResources.configs;
	const requestOptions = resolveInitialListPageRequest(resource.persistKey, {
		column: 'name',
		direction: 'asc'
	});

	const result = await tryCatch(
		(async () => {
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const items = await queryClient.query(swarmKvListQuery(swarm, resource.kind, requestOptions));
			return { swarm, items };
		})()
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: resource.title() }));
	}

	return {
		swarm: result.data.swarm,
		items: result.data.items,
		requestOptions
	};
};
