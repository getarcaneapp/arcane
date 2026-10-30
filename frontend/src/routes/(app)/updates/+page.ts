import { containerListQuery } from '#lib/query/containers.js';
import { projectListQuery } from '#lib/query/projects.js';
import type { ContainerListRequestOptions } from '#lib/services/container-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
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
		Promise.all([
			queryClient.query(containerListQuery(scope, readableEnvironments('containers:list'), containerRequestOptions)),
			queryClient.query(projectListQuery(scope, readableEnvironments('projects:list'), projectRequestOptions))
		])
	);
	if (operationResult.error !== null) {
		throwPageLoadError(operationResult.error, 'Failed to load updates');
	}

	return {
		containerRequestOptions,
		projectRequestOptions
	};
};
