import { gitopsRepositoryService } from '$lib/services/gitops-repository-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';

export const load = async () => {
	const repositoryRequestOptions: SearchPaginationSortRequest = {
		page: 1,
		perPage: 10,
		sortBy: 'createdAt',
		sortOrder: 'desc'
	};

	const repositories = await gitopsRepositoryService.getRepositories(repositoryRequestOptions);

	return {
		repositories,
		repositoryRequestOptions
	};
};
