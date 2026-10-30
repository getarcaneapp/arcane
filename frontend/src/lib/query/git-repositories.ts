import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { gitRepositoryService } from '#lib/services/git-repository-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function gitRepositoryListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.gitRepositories.list(requestOptions),
		queryFn: () => gitRepositoryService.getRepositories(requestOptions)
	});
}

export function gitRepositoryBranchesQuery(repositoryId: string) {
	return queryOptions({
		queryKey: queryKeys.gitRepositories.branches(repositoryId),
		queryFn: () => gitRepositoryService.getBranches(repositoryId)
	});
}
