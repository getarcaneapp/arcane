import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	await environmentStore.ready;
	const environments = readableEnvironments('containers:list');
	const scope = environmentStore.shownKey;

	const containerRequestOptions = resolveInitialTableRequest('arcane-container-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'created', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);

	const listOptions = { ...containerRequestOptions, groupByProject: false };
	let containers;
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.containers.list(scope, listOptions),
				queryFn: () =>
					fetchAcrossEnvironments(environments, listOptions, (id, request, config) =>
						containerService.getContainersForEnvironment(id, request, config)
					)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load containers');
	} else {
		containers = operationResult.data;
	}

	return {
		containers,
		containerRequestOptions
	};
};
