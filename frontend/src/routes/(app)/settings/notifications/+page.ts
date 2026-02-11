import { notificationService } from '$lib/services/notification-service';
import { queryKeys } from '$lib/query/query-keys';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	try {
		const notificationSettings = await queryClient.fetchQuery({
			queryKey: queryKeys.notifications.settings(),
			queryFn: () => notificationService.getSettings()
		});

		return {
			notificationSettings
		};
	} catch (error) {
		console.error('Failed to load notification settings:', error);
		throw error;
	}
};
