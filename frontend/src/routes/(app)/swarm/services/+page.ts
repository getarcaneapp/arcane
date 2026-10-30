import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { swarmServiceListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const requestOptions = resolveInitialListPageRequest('arcane-swarm-services-table', {
		column: 'name',
		direction: 'asc'
	});

	const result = await tryCatch(
		(async () => {
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const services = await queryClient.query(swarmServiceListQuery(swarm, requestOptions));
			return { swarm, services };
		})()
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.services() }));
	}

	return {
		swarm: result.data.swarm,
		services: result.data.services,
		requestOptions
	};
};
