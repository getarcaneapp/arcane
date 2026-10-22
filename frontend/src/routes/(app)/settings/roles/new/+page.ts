import { redirect } from '@sveltejs/kit';

import { PERMISSIONS } from '#lib/permissions.generated.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { roleService } from '#lib/services/role-service.js';
import { userHasPermission } from '#lib/utils/auth.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();

	if (!userHasPermission(user, PERMISSIONS.ROLES_CREATE)) {
		throw redirect(302, '/settings/roles');
	}

	const permissionsManifest = await queryClient.query({
		queryKey: queryKeys.roles.permissionsManifest(),
		queryFn: () => roleService.getPermissionsManifest()
	});

	return {
		permissionsManifest
	};
};
