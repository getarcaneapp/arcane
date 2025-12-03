import { userService } from '$lib/services/user-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';

export const load = async () => {
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

	const users = await userService.getUsers(userRequestOptions);

	return {
		users,
		userRequestOptions
	};
};
