import type { PageLoad } from './$types';
import { loadMergedSettingsPage } from '#lib/utils/settings-load.js';

export const load: PageLoad = async ({ parent }) => {
	return loadMergedSettingsPage(parent, 'activity settings');
};
