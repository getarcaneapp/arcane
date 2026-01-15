import type { PageLoad } from './$types';
import { settingsService } from '$lib/services/settings-service';

export const load: PageLoad = async () => {
	try {
		const settings = await settingsService.getSettings();
		return { settings };
	} catch (error) {
		console.error('Failed to load timeout settings:', error);
		throw error;
	}
};
