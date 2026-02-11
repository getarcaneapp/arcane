import { projectService } from '$lib/services/project-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

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

	const [projects, projectStatusCounts] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.projects.list(envId, projectRequestOptions),
			queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.projects.statusCounts(envId),
			queryFn: () => projectService.getProjectStatusCountsForEnvironment(envId)
		})
	]);

	return { projects, projectRequestOptions, projectStatusCounts };
};
