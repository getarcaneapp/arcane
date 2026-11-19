import { m } from '#lib/paraglide/messages.js';
import { swarmEnvironmentsQuery } from '#lib/query/swarm.js';
import { swarmNodeListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
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

	const swarm = await tryCatch(queryClient.query(swarmEnvironmentsQuery('swarm-nodes:list')));
	if (swarm.error !== null) {
		throwPageLoadError(swarm.error, m.common_load_failed({ resource: m.nodes() }));
	}
	const nodes = await tryCatch(queryClient.query(swarmNodeListQuery(swarm.data, requestOptions)));
	if (nodes.error !== null) {
		throwPageLoadError(nodes.error, m.common_load_failed({ resource: m.nodes() }));
	}

	return {
		swarm: swarm.data,
		nodes: nodes.data,
		requestOptions
	};
};
