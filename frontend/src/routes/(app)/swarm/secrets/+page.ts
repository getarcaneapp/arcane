import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
import { swarmKvResources } from '#lib/utils/swarm-kv.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const resource = swarmKvResources.secrets;
	const requestOptions = resolveInitialListPageRequest(resource.persistKey, {
		column: 'name',
		direction: 'asc'
	});

	const result = await tryCatch(
		(async () => {
			await environmentStore.ready;
			const swarm = await queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
			const items = await queryClient.query({
				queryKey: queryKeys.swarm[resource.kind](swarm.scope, requestOptions),
				queryFn: async () =>
					withFailedEnvironments(
						await fetchAcrossEnvironments(swarm.environments, requestOptions, (id, request, config) =>
							swarmService.getKvItems(resource.kind, request, id, config)
						),
						swarm.failedEnvironments
					)
			});
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
