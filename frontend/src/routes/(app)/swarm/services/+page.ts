import { m } from '#lib/paraglide/messages.js';
import { swarmEnvironmentsQuery } from '#lib/query/swarm.js';
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

	const swarm = await tryCatch(queryClient.query(swarmEnvironmentsQuery('swarm-services:list')));
	if (swarm.error !== null) {
		throwPageLoadError(swarm.error, m.common_load_failed({ resource: m.services() }));
	}
	const services = await tryCatch(queryClient.query(swarmServiceListQuery(swarm.data, requestOptions)));
	if (services.error !== null) {
		throwPageLoadError(services.error, m.common_load_failed({ resource: m.services() }));
	}

	return {
		swarm: swarm.data,
		services: services.data,
		requestOptions
	};
};
