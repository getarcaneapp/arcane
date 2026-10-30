import { environmentListQuery } from '#lib/query/environments.js';
import { roleAllQuery } from '#lib/query/roles.js';
import { userListQuery } from '#lib/query/users.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

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
		queryClient.query(userListQuery(userRequestOptions)),
		queryClient.query({ ...roleAllQuery(), staleTime: 30_000 }),
		queryClient.query({ ...environmentListQuery({ pagination: { page: 1, limit: 1000 } }), staleTime: 30_000 })
	]);

	return {
		users,
		userRequestOptions,
		roles,
		environments: environmentsPage.data
	};
};
