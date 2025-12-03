import { settingsService } from '$lib/services/settings-service';

export const load = async ({ parent }) => {
	try {
		const { settings } = await parent();
		const oidcStatus = await settingsService.getOidcStatus();

		return {
			settings,
			oidcStatus
		};
	} catch (error) {
		console.error('Failed to load OIDC status:', error);
		throw error;
	}
};
