import { settingsService } from '#lib/services/settings-service.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export const load = async ({ parent }) => {
	const operationResult = await tryCatch(Promise.all([parent(), settingsService.getOidcStatus()]));
	if (operationResult.error !== null) {
		const error = operationResult.error;

		console.error('Failed to load OIDC status:', error);
		throw error;
	} else {
		const [{ settings }, oidcStatus] = operationResult.data;
		return {
			settings,
			oidcStatus
		};
	}
};
