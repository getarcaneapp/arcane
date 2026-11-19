import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { networkDetailQuery } from '#lib/query/networks.js';
import { extractApiErrorStatus } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);

	const { networkId } = params;

	const network = await tryCatch(queryClient.query(networkDetailQuery(envId, networkId)));
	if (network.error !== null) {
		console.error('Failed to load network:', network.error);
		if (extractApiErrorStatus(network.error) === 404) {
			throw network.error;
		}
		throw error(500, network.error.message || m.failed_to_load_network_details());
	}
	if (!network.data) {
		throw error(404, 'Network not found');
	}

	return {
		envId,
		network: network.data
	};
};
