import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { swarmServiceQuery } from '#lib/query/swarm.js';
import { extractApiErrorStatus } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const serviceId = params.serviceId;
	const [{ queryClient }, envId] = await Promise.all([parent(), resolveDetailEnvironmentId(url)]);

	const service = await tryCatch(queryClient.query({ ...swarmServiceQuery(envId, serviceId), staleTime: 0, retry: false }));
	if (service.error !== null) {
		console.error('Failed to load service:', service.error);
		if (extractApiErrorStatus(service.error) === 404) {
			throw service.error;
		}
		throw error(500, service.error.message || m.failed_to_load_service_details());
	}
	if (!service.data) {
		throw error(404, 'Service not found');
	}

	return { envId, serviceId, service: service.data };
};
