import { settingsService } from '#lib/services/settings-service.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export const load = async ({ parent }) => {
	const operationResult = await tryCatch(
		(async () => {
			const { settings } = await parent();
			const oidcStatus = await settingsService.getOidcStatus();

			return {
				settings,
				oidcStatus
			};
		})()
	);
	if (operationResult.error !== null) {
		const error = operationResult.error;

		console.error('Failed to load OIDC status:', error);
		throw error;
	} else {
		return operationResult.data;
	}
};
