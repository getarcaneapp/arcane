import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { settingsService } from '$lib/services/settings-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params }) => {
	try {
		const environment = await environmentManagementService.get(params.id);

		let settings = null;
		try {
			settings = await settingsService.getSettingsForEnvironment(params.id);
		} catch {}

		return {
			environment,
			settings
		};
	} catch (error) {
		console.error('Failed to load environment:', error);
		throw error;
	}
};
