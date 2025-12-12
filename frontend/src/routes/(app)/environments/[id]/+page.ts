import { environmentManagementService } from '$lib/services/env-mgmt-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params }) => {
	try {
		const environment = await environmentManagementService.get(params.id);

		return {
			environment
		};
	} catch (error) {
		console.error('Failed to load environment:', error);
		throw error;
	}
};
