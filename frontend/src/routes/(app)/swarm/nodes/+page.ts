import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { swarmNodeListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const requestOptions = resolveInitialTableRequest('arcane-swarm-nodes-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'hostname',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const result = await tryCatch(
		(async () => {
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const nodes = await queryClient.query(swarmNodeListQuery(swarm, requestOptions));
			return { swarm, nodes };
		})()
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.nodes() }));
	}

	return {
		swarm: result.data.swarm,
		nodes: result.data.nodes,
		requestOptions
	};
};
