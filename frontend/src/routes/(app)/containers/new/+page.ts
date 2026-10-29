import { redirect } from '@sveltejs/kit';

import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { withBase } from '#lib/utils/base-path.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { user } = await parent();
	await environmentStore.ready;
	if (!environmentStore.shown.some((env) => userHasPermission(user, 'containers:create', env.id))) {
		redirect(302, withBase('/containers'));
	}
};
