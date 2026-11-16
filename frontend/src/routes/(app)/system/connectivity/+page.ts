import settingsStore from '#lib/stores/config-store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	await parent();
	await settingsStore.reload().catch((err: unknown) => throwPageLoadError(err, 'Failed to load settings'));
};
