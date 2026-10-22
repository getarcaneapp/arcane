import { PERMISSIONS } from '#lib/permissions.generated.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { groupService } from '#lib/services/group-service.js';
import { roleService } from '#lib/services/role-service.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { hasPermission } from '#lib/utils/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params }) => {
	const { queryClient } = await parent();

	const result = await tryCatch(
		Promise.all([
			queryClient.query({
				queryKey: queryKeys.groups.detail(params.id),
				queryFn: () => groupService.get(params.id)
			}),
			queryClient.query({
				queryKey: queryKeys.groups.members(params.id),
				queryFn: () => groupService.getMembers(params.id)
			}),
			hasPermission(PERMISSIONS.ROLES_LIST)
				? queryClient.query({
						queryKey: queryKeys.roles.all(),
						queryFn: () => roleService.getAll(),
						staleTime: 30_000
					})
				: null,
			queryClient.query({
				queryKey: queryKeys.environments.list({ pagination: { page: 1, limit: -1 } }),
				queryFn: () => environmentManagementService.getEnvironments({ pagination: { page: 1, limit: -1 } }),
				staleTime: 30_000
			})
		])
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, 'Failed to load group');
	}

	const [group, members, roles, environmentsPage] = result.data;
	return {
		group,
		members,
		roles,
		environments: environmentsPage.data
	};
};
