import { tryCatch } from '#lib/utils/try-catch.js';
import { networkService } from '#lib/services/network-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { resolveListPageLoadContext } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const {
		queryClient,
		envId,
		requestOptions: networkRequestOptions
	} = await resolveListPageLoadContext(parent, 'arcane-networks-table', {
		column: 'name',
		direction: 'asc'
	});

	// Single API call - counts are included in the response
	let networks;
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.networks.list(envId, networkRequestOptions),
				queryFn: () => networkService.getNetworksForEnvironment(envId, networkRequestOptions)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load networks');
	} else {
		networks = operationResult.data;
	}

	return {
		envId,
		networks,
		networkRequestOptions,
		// Use counts from the networks response
		networkUsageCounts: networks.counts ?? { inuse: 0, unused: 0, total: 0 }
	};
};
