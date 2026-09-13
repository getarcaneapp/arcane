import { tryCatch } from '#lib/utils/try-catch.js';
import { notificationService } from '#lib/services/notification-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const operationResult = await tryCatch(
		(async () => {
			const notificationSettings = await queryClient.query({
				queryKey: queryKeys.notifications.settings(),
				queryFn: () => notificationService.getSettings()
			});

			return {
				notificationSettings
			};
		})()
	);
	if (operationResult.error !== null) {
		const error = operationResult.error;

		console.error('Failed to load notification settings:', error);
		throw error;
	} else {
		return operationResult.data;
	}
};
