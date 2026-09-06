import { redirect } from '@sveltejs/kit';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { PageLoad } from './$types';

// Job schedules are configured per environment; this route exists so the
// settings landing and search can link to a stable /settings URL.
export const load: PageLoad = async () => {
	const envId = await environmentStore.getCurrentEnvironmentId().catch(() => '0');
	redirect(302, `/environments/${envId}?tab=jobs`);
};
