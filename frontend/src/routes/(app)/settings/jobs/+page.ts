import { redirect } from '@sveltejs/kit';

import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { withBase } from '#lib/utils/base-path.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

// Job schedules are configured per environment; this route exists so the
// settings landing and search can link to a stable /settings URL.
export const load: PageLoad = async () => {
	const envId = await tryCatch(environmentStore.getCurrentEnvironmentId()).then((result) => (result.error ? '0' : result.data));
	redirect(302, withBase(`/environments/${envId}?tab=jobs`));
};
