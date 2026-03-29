import { networkService } from '$lib/services/network-service';
import { queryKeys } from '$lib/query/query-keys';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	let topology;
	try {
		topology = await queryClient.fetchQuery({
			queryKey: queryKeys.networks.topology(envId),
			queryFn: () => networkService.getNetworkTopology(envId)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load network topology');
	}

	return {
		topology
	};
};
