import { gitopsRepositoryService } from '$lib/services/gitops-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';

export const load = async () => {
	const repositoryRequestOptions: SearchPaginationSortRequest = {
		sort: {
			column: 'createdAt',
			direction: 'desc' as const
		}
	};

	const repositories = await gitopsRepositoryService.getRepositories(repositoryRequestOptions);

	return {
		repositories,
		repositoryRequestOptions
	};
};
