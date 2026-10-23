import { error } from '@sveltejs/kit';

import { swarmService } from '#lib/services/swarm-service.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, url }) => {
	const serviceId = params.serviceId;
	const envId = await resolveDetailEnvironmentId(url);

	const operationResult = await tryCatch(
		(async () => {
			const service = await swarmService.getService(serviceId, envId);

			if (!service) {
				throw error(404, 'Service not found');
			}

			return { envId, service };
		})()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		console.error('Failed to load service:', err);
		if ('status' in err && err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load service details');
	} else {
		return operationResult.data;
	}
};
