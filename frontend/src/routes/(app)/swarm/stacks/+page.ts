import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { swarmStackListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const requestOptions = resolveInitialListPageRequest('arcane-swarm-stacks-table', {
		column: 'name',
		direction: 'asc'
	});

	const result = await tryCatch(
		(async () => {
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const stacks = await queryClient.query(swarmStackListQuery(swarm, requestOptions));
			return { swarm, stacks };
		})()
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.swarm_stacks_title() }));
	}

	return {
		swarm: result.data.swarm,
		stacks: result.data.stacks,
		requestOptions
	};
};
