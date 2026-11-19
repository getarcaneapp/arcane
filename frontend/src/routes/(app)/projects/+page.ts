import { projectListQuery, projectStatusCountsQuery } from '#lib/query/projects.js';
import { projectTagsQuery } from '#lib/query/projects.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

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

	const projectData = await tryCatch(
		Promise.all([
			queryClient.ensureQueryData({ ...projectListQuery(scope, environments, projectRequestOptions), revalidateIfStale: true }),
			queryClient.ensureQueryData({ ...projectStatusCountsQuery(scope, environments), revalidateIfStale: true }),
			queryClient.ensureQueryData({ ...projectTagsQuery(scope, environments), revalidateIfStale: true })
		])
	);
	if (projectData.error !== null) {
		throwPageLoadError(projectData.error, 'Failed to load projects');
	}
	const [projects, projectStatusCounts, projectTags] = projectData.data;

	return { scope, projects, projectRequestOptions, projectStatusCounts, projectTags, showArchived };
};
