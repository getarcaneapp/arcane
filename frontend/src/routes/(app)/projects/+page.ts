import { projectService } from '$lib/services/project-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
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
		projectService.getProjects(projectRequestOptions),
		projectService.getProjectStatusCounts()
	]);

	return { projects, projectRequestOptions, projectStatusCounts };
};
