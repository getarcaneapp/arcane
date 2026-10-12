import { error } from '@sveltejs/kit';

import { queryKeys } from '#lib/query/query-keys.js';
import { networkService } from '#lib/services/network-service.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);

	const { networkId } = params;

	const operationResult = await tryCatch(
		(async () => {
			const network = await queryClient.query({
				queryKey: queryKeys.networks.detail(envId, networkId),
				queryFn: () => networkService.getNetworkForEnvironment(envId, networkId)
			});

			if (!network) {
				throw error(404, 'Network not found');
			}

			return {
				envId,
				network
			};
		})()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		console.error('Failed to load network:', err);
		if ('status' in err && err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load network details');
	} else {
		return operationResult.data;
	}
};
