import { queryKeys } from '#lib/query/query-keys.js';
import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service.js';
import { projectService } from '#lib/services/project-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const environments = await environmentStore.getShownEnvironments();
	const scope = environmentStore.shownKey;

	const containerRequestOptions = ensureStandaloneContainerUpdatesFilter(
		resolveInitialTableRequest('arcane-updates-container-table', {
			pagination: { page: 1, limit: 100 },
			sort: { column: 'created', direction: 'desc' }
		} satisfies SearchPaginationSortRequest)
	) as ContainerListRequestOptions;

	const projectRequestOptions = ensureUpdatesFilter(
		resolveInitialTableRequest('arcane-updates-project-table', {
			pagination: { page: 1, limit: 20 },
			sort: { column: 'name', direction: 'asc' }
		} satisfies SearchPaginationSortRequest)
	);

	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.containers.list(scope, containerRequestOptions),
					queryFn: () =>
						fetchAcrossEnvironments(environments, containerRequestOptions, (id, request, config) =>
							containerService.getContainersForEnvironment(id, request, config)
						)
				}),
				queryClient.query({
					queryKey: queryKeys.projects.list(scope, projectRequestOptions),
					queryFn: () =>
						fetchAcrossEnvironments(environments, projectRequestOptions, (id, request, config) =>
							projectService.getProjectsForEnvironment(id, request, config)
						)
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load updates');
	}

	return {
		containerRequestOptions,
		projectRequestOptions
	};
};
