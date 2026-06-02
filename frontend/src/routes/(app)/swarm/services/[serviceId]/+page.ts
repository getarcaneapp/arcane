import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { swarmService } from '$lib/services/swarm-service';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const serviceId = params.serviceId;

	try {
		const service = await queryClient.fetchQuery({
			queryKey: queryKeys.swarm.services.detail(serviceId),
			queryFn: () => swarmService.getService(serviceId)
		});

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
