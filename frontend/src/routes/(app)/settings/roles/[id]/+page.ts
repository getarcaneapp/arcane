import { error, redirect } from '@sveltejs/kit';

import { permissionsManifestQuery, roleDetailQuery } from '#lib/query/roles.js';
import { userIsGlobalAdmin } from '#lib/utils/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params }) => {
	const { queryClient, user } = await parent();

	// Role editing is reserved for global admins (matches RequireGlobalAdmin on
	// PATCH /roles/{id}). Guard here so the role payload isn't fetched for users
	// who'll be redirected by the layout effect anyway.
	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/settings/roles');
	}

	const [role, permissionsManifest] = await Promise.all([
		tryCatch(queryClient.query(roleDetailQuery(params.id))).then((result) => (result.error ? null : result.data)),
		queryClient.query(permissionsManifestQuery())
	]);

	if (!role) {
		throw error(404, 'Role not found');
	}

	return {
		role,
		permissionsManifest
	};
};
