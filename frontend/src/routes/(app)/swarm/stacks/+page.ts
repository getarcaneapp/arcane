import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const requestOptions = resolveInitialListPageRequest('arcane-swarm-stacks-table', {
		column: 'name',
		direction: 'asc'
	});

	const result = await tryCatch(
		(async () => {
			await environmentStore.ready;
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const stacks = await queryClient.query({
				queryKey: queryKeys.swarm.stacks(swarm.managers.map((env) => env.id).join(','), requestOptions),
				queryFn: async () =>
					withFailedEnvironments(
						await fetchAcrossEnvironments(swarm.managers, requestOptions, (id, request, config) =>
							swarmService.getStacks(request, id, config)
						),
						swarm.failedEnvironments
					)
			});
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
