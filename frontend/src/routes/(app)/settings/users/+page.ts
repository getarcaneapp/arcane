import { userService } from '$lib/services/user-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const userRequestOptions = resolveInitialTableRequest('arcane-users-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'Username',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const users = await queryClient.fetchQuery({
		queryKey: queryKeys.users.list(userRequestOptions),
		queryFn: () => userService.getUsers(userRequestOptions)
	});

	return {
		users,
		userRequestOptions
	};
};
