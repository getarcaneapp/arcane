import { userService } from '$lib/services/user-service';
import { roleService } from '$lib/services/role-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
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

	const [users, roles, environmentsPage] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.users.list(userRequestOptions),
			queryFn: () => userService.getUsers(userRequestOptions)
		}),
		queryClient.fetchQuery({
			queryKey: ['roles', 'all'],
			queryFn: () => roleService.getAll(),
			staleTime: 30_000
		}),
		queryClient.fetchQuery({
			queryKey: ['environments', 'all-for-role-assignments'],
			queryFn: () => environmentManagementService.getEnvironments({ pagination: { page: 1, limit: 1000 } }),
			staleTime: 30_000
		})
	]);

	return {
		users,
		userRequestOptions,
		roles,
		environments: environmentsPage.data
	};
};
