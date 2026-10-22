import { redirect } from '@sveltejs/kit';

import { PERMISSIONS } from '#lib/permissions.generated.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { user } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const shown = await environmentStore.getShownEnvironments();
	if (!shown.some((env) => userHasPermission(user, PERMISSIONS.CONTAINERS_CREATE, env.id))) {
		redirect(302, '/containers');
	}

	return { envId };
};
