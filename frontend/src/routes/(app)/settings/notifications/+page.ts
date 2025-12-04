import { notificationService } from '$lib/services/notification-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	try {
		const notificationSettings = await notificationService.getSettings();

		return {
			notificationSettings
		};
	} catch (error) {
		console.error('Failed to load notification settings:', error);
		throw error;
	}
};
