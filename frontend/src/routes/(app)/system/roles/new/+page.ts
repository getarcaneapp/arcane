import { redirect } from '@sveltejs/kit';

import { permissionsManifestQuery } from '#lib/query/roles.js';
import { userIsGlobalAdmin } from '#lib/utils/auth.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();

	if (!userIsGlobalAdmin(user)) {
		throw redirect(302, '/system/roles');
	}

	const permissionsManifest = await queryClient.query(permissionsManifestQuery());

	return {
		permissionsManifest
	};
};
