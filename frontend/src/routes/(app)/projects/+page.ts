import { queryKeys } from '#lib/query/query-keys.js';
import { projectService } from '#lib/services/project-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';
import { fetchProjectTags } from './projects-table.helpers';

export const load: PageLoad = async ({ parent, url }) => {
	const { queryClient } = await parent();
	const environments = await environmentStore.getShownEnvironments();
	const scope = environmentStore.shownKey;
	const showArchived = url.searchParams.get('archived') === 'true';

	const projectRequestOptions = resolveInitialTableRequest('arcane-project-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);
	const filters = { ...(projectRequestOptions.filters ?? {}) };
	if (showArchived) {
		filters['archived'] = 'true';
	} else {
		delete filters['archived'];
	}
	projectRequestOptions.filters = Object.keys(filters).length ? filters : undefined;

	let projects;
	let projectStatusCounts;
	let projectTags;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.projects.list(scope, projectRequestOptions),
					queryFn: () =>
						fetchAcrossEnvironments(environments, projectRequestOptions, (id, request, config) =>
							projectService.getProjectsForEnvironment(id, request, config)
						)
				}),
				queryClient.query({
					queryKey: queryKeys.projects.statusCounts(scope),
					queryFn: () =>
						sumAcrossEnvironments(environments, (id, config) => projectService.getProjectStatusCountsForEnvironment(id, config))
				}),
				queryClient.query({
					queryKey: queryKeys.projects.tags(scope),
					queryFn: () => fetchProjectTags(environments)
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load projects');
	} else {
		[projects, projectStatusCounts, projectTags] = operationResult.data;
	}

	return { scope, projects, projectRequestOptions, projectStatusCounts, projectTags, showArchived };
};
