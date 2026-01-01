import { notificationService } from '$lib/services/notification-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	try {
		const [notificationSettings, appriseSettings] = await Promise.all([
			notificationService.getSettings(),
			notificationService.getAppriseSettings()
		]);

		return {
			notificationSettings,
			appriseSettings
		};
	} catch (error) {
		console.error('Failed to load notification settings:', error);
		throw error;
	}
};
