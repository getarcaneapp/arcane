import { projectService } from '#lib/services/project-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import type { PageLoad } from './$types';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

export const load: PageLoad = async ({ parent, url }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
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
	try {
		[projects, projectStatusCounts, projectTags] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.projects.list(envId, projectRequestOptions),
				queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.projects.statusCounts(envId),
				queryFn: () => projectService.getProjectStatusCountsForEnvironment(envId)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.projects.tags(envId),
				queryFn: () => projectService.getProjectTagsForEnvironment(envId)
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load projects');
	}

	return { envId, projects, projectRequestOptions, projectStatusCounts, projectTags, showArchived };
};
