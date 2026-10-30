import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { userService } from '#lib/services/user-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function userListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.users.list(requestOptions),
		queryFn: () => userService.getUsers(requestOptions)
	});
}
