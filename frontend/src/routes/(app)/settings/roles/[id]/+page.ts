import { error } from '@sveltejs/kit';

import { queryKeys } from '#lib/query/query-keys.js';
import { roleService } from '#lib/services/role-service.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params }) => {
	const { queryClient } = await parent();

	const [role, permissionsManifest] = await Promise.all([
		tryCatch(
			queryClient.query({
				queryKey: queryKeys.roles.detail(params.id),
				queryFn: () => roleService.get(params.id)
			})
		).then((result) => (result.error ? null : result.data)),
		queryClient.query({
			queryKey: queryKeys.roles.permissionsManifest(),
			queryFn: () => roleService.getPermissionsManifest()
		})
	]);

	if (!role) {
		throw error(404, 'Role not found');
	}

	return {
		role,
		permissionsManifest
	};
};
