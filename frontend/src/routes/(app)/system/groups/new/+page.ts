import { redirect } from '@sveltejs/kit';

import { environmentListQuery } from '#lib/query/environments.js';
import { roleAllQuery } from '#lib/query/roles.js';
import { userListQuery } from '#lib/query/users.js';
import { userIsGlobalAdmin } from '#lib/utils/auth.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();

	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/system/groups');
	}

	const [usersPage, roles, environmentsPage] = await Promise.all([
		queryClient.query(userListQuery({ pagination: { page: 1, limit: -1 }, sort: { column: 'Username', direction: 'asc' } })),
		queryClient.query({ ...roleAllQuery(), staleTime: 30_000 }),
		queryClient.query({ ...environmentListQuery({ pagination: { page: 1, limit: -1 } }), staleTime: 30_000 })
	]);

	return {
		users: usersPage.data,
		roles,
		environments: environmentsPage.data
	};
};
