import { error, redirect } from '@sveltejs/kit';

import { environmentListQuery } from '#lib/query/environments.js';
import { groupDetailQuery, groupMembersQuery, groupRoleAssignmentsQuery } from '#lib/query/groups.js';
import { roleAllQuery } from '#lib/query/roles.js';
import { userListQuery } from '#lib/query/users.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { userIsGlobalAdmin } from '#lib/utils/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params }) => {
	const { queryClient, user } = await parent();

	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/system/groups');
	}

	const groupResult = await tryCatch(queryClient.query(groupDetailQuery(params.id)));
	if (groupResult.error !== null) {
		throwPageLoadError(groupResult.error, 'Failed to load group');
	}
	if (!groupResult.data) {
		error(404, 'Group not found');
	}

	const [members, roleAssignments, usersPage, roles, environmentsPage] = await Promise.all([
		queryClient.query(groupMembersQuery(params.id)),
		queryClient.query(groupRoleAssignmentsQuery(params.id)),
		queryClient.query(userListQuery({ pagination: { page: 1, limit: -1 }, sort: { column: 'Username', direction: 'asc' } })),
		queryClient.query({ ...roleAllQuery(), staleTime: 30_000 }),
		queryClient.query({ ...environmentListQuery({ pagination: { page: 1, limit: -1 } }), staleTime: 30_000 })
	]);

	return {
		group: groupResult.data,
		members,
		roleAssignments,
		users: usersPage.data,
		roles,
		environments: environmentsPage.data
	};
};
