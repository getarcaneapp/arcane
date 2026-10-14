import { queryKeys } from '#lib/query/query-keys.js';
import { volumeService } from '#lib/services/volume-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const environments = await environmentStore.getShownEnvironments();
	const scope = environmentStore.shownKey;

	const volumeRequestOptions = resolveInitialTableRequest(
		'arcane-volumes-table',
		{
			pagination: {
				page: 1,
				limit: 20
			},
			sort: {
				column: 'name',
				direction: 'asc'
			}
		} satisfies SearchPaginationSortRequest,
		{ deferredSortColumns: ['size'] }
	);

	// Single API call - counts are included in the response
	const volumes = await queryClient.query({
		queryKey: queryKeys.volumes.table(scope, volumeRequestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, volumeRequestOptions, (id, request, config) =>
				volumeService.getVolumesForEnvironment(id, request, config)
			)
	});

	return {
		scope,
		volumes,
		volumeRequestOptions,
		// Use counts from the volumes response
		volumeUsageCounts: volumes.counts ?? { inuse: 0, unused: 0, total: 0 }
	};
};
