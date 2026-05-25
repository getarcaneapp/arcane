import { error, redirect } from '@sveltejs/kit';
import { roleService } from '$lib/services/role-service';
import { userIsGlobalAdmin } from '$lib/utils/auth';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params }) => {
	const { queryClient, user } = await parent();

	// Role editing is reserved for global admins (matches RequireGlobalAdmin on
	// PUT /roles/{id}). Guard here so the role payload isn't fetched for users
	// who'll be redirected by the layout effect anyway.
	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/settings/roles');
	}

	const [role, permissionsManifest] = await Promise.all([
		queryClient
			.fetchQuery({
				queryKey: ['roles', 'detail', params.id],
				queryFn: () => roleService.get(params.id)
			})
			.catch(() => null),
		queryClient.fetchQuery({
			queryKey: ['roles', 'permissions-manifest'],
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
