import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { containerService } from '#lib/services/container-service.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import type { PageLoad } from './$types';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { throwPageLoadError } from '#lib/utils/api.js';

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
