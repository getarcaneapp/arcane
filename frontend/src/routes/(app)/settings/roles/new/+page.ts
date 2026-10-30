import { redirect } from '@sveltejs/kit';

import { permissionsManifestQuery } from '#lib/query/roles.js';
import { userIsGlobalAdmin } from '#lib/utils/auth.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();

	// Role creation is reserved for global admins (matches RequireGlobalAdmin
	// on POST /roles). Guard here so non-admins are bounced before the
	// permissions manifest is fetched.
	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/settings/roles');
	}

	const permissionsManifest = await queryClient.query(permissionsManifestQuery());

	return {
		permissionsManifest
	};
};
