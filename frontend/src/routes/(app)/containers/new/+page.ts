import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import { hasPermission } from '#lib/utils/auth';

export const load: PageLoad = async () => {
	const envId = await environmentStore.getCurrentEnvironmentId();
	if (!hasPermission('containers:create', envId)) {
		redirect(302, '/containers');
	}

	return { envId };
};
