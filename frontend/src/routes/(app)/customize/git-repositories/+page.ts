import { gitRepositoryService } from '$lib/services/git-repository-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const repositoryRequestOptions = resolveInitialTableRequest('arcane-git-repositories-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const repositories = await gitRepositoryService.getRepositories(repositoryRequestOptions);

	return { repositories, repositoryRequestOptions };
};
