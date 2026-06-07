import type { PageLoad } from './$types';
import { loadMergedSettingsPage } from '$lib/utils/settings-load';

export const load: PageLoad = async ({ parent }) => {
	return loadMergedSettingsPage(parent, 'timeout settings');
};
