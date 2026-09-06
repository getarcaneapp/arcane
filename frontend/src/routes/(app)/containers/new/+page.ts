import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';

export const load: PageLoad = async ({ parent }) => {
	const { user } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	if (!userHasPermission(user, 'containers:create', envId)) {
		redirect(302, '/containers');
	}

	return { envId };
};
