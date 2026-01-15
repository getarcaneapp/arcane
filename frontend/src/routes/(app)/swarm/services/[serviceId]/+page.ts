import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { swarmService } from '$lib/services/swarm-service';

export const load: PageLoad = async ({ params }) => {
	const serviceId = params.serviceId;

	try {
		const service = await swarmService.getService(serviceId);

		if (!service) {
			throw error(404, 'Service not found');
		}

		return { service };
	} catch (err: any) {
		console.error('Failed to load service:', err);
		if (err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load service details');
	}
};
