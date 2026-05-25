import { redirect } from '@sveltejs/kit';
import { roleService } from '$lib/services/role-service';
import { userIsGlobalAdmin } from '$lib/utils/auth';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();

	// Role creation is reserved for global admins (matches RequireGlobalAdmin
	// on POST /roles). Guard here so non-admins are bounced before the
	// permissions manifest is fetched.
	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/settings/roles');
	}

	const permissionsManifest = await queryClient.fetchQuery({
		queryKey: ['roles', 'permissions-manifest'],
		queryFn: () => roleService.getPermissionsManifest()
	});

	return {
		permissionsManifest,
		role: null
	};
};
