import { gitRepositoryService } from '$lib/services/git-repository-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

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

	const repositories = await queryClient.fetchQuery({
		queryKey: queryKeys.gitRepositories.list(repositoryRequestOptions),
		queryFn: () => gitRepositoryService.getRepositories(repositoryRequestOptions)
	});

	return { repositories, repositoryRequestOptions };
};
