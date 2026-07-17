import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { containerService } from '$lib/services/container-service';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import { throwPageLoadError } from '$lib/utils/api';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const containerRequestOptions = resolveInitialTableRequest('arcane-container-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'created', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);

	let containers;
	try {
		containers = await queryClient.fetchQuery({
			queryKey: queryKeys.containers.list(envId, containerRequestOptions),
			queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load containers');
	}

	return {
		envId,
		containers,
		containerRequestOptions
	};
};
