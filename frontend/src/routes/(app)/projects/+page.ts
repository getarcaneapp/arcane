import { projectListQuery, projectStatusCountsQuery } from '#lib/query/projects.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';
import { fetchProjectTags } from './projects-table.helpers';

export const load: PageLoad = async ({ parent, url }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const environments = readableEnvironments('projects:list');
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
	const filters = { ...projectRequestOptions.filters };
	if (showArchived) {
		filters['archived'] = 'true';
	} else {
		delete filters['archived'];
	}
	projectRequestOptions.filters = Object.keys(filters).length ? filters : undefined;

	const operationResult = await tryCatch(
		Promise.all([
			queryClient.query(projectListQuery(scope, environments, projectRequestOptions)),
			queryClient.query(projectStatusCountsQuery(scope, environments)),
			queryClient.query({
				queryKey: queryKeys.projects.tags(scope),
				queryFn: () => fetchProjectTags(environments)
			})
		])
	);
	if (operationResult.error !== null) {
		throwPageLoadError(operationResult.error, 'Failed to load projects');
	}
	const [projects, projectStatusCounts, projectTags] = operationResult.data;

	return { scope, projects, projectRequestOptions, projectStatusCounts, projectTags, showArchived };
};
