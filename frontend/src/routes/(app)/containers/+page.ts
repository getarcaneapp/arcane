import { tryCatch } from '#lib/utils/try-catch.js';
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
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.containers.list(envId, containerRequestOptions),
				queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load containers');
	} else {
		containers = operationResult.data;
	}

	return {
		envId,
		containers,
		containerRequestOptions
	};
};
