import { volumeService } from '$lib/services/volume-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const volumeRequestOptions = resolveInitialTableRequest('arcane-volumes-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	// Single API call - counts are included in the response
	const volumes = await queryClient.fetchQuery({
		queryKey: queryKeys.volumes.table(envId, volumeRequestOptions),
		queryFn: () => volumeService.getVolumesForEnvironment(envId, volumeRequestOptions)
	});

	return {
		volumes,
		volumeRequestOptions,
		// Use counts from the volumes response
		volumeUsageCounts: volumes.counts ?? { inuse: 0, unused: 0, total: 0 }
	};
};
