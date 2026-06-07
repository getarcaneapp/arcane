import { networkService } from '$lib/services/network-service';
import { queryKeys } from '$lib/query/query-keys';
import { resolveListPageLoadContext } from '$lib/utils/tables';
import { throwPageLoadError } from '$lib/utils/api';
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
	try {
		networks = await queryClient.fetchQuery({
			queryKey: queryKeys.networks.list(envId, networkRequestOptions),
			queryFn: () => networkService.getNetworksForEnvironment(envId, networkRequestOptions)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load networks');
	}

	return {
		networks,
		networkRequestOptions,
		// Use counts from the networks response
		networkUsageCounts: networks.counts ?? { inuse: 0, unused: 0, total: 0 }
	};
};
