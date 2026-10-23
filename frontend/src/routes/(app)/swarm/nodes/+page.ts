import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
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
			await environmentStore.ready;
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const nodes = await queryClient.query({
				queryKey: queryKeys.swarm.nodes(swarm.scope, requestOptions),
				queryFn: async () =>
					withFailedEnvironments(
						await fetchAcrossEnvironments(swarm.environments, requestOptions, (id, request, config) =>
							swarmService.getNodes(request, id, config)
						),
						swarm.failedEnvironments
					)
			});
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
