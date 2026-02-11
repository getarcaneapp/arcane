import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { networkService } from '$lib/services/network-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const { networkId } = params;

	try {
		const network = await queryClient.fetchQuery({
			queryKey: queryKeys.networks.detail(envId, networkId),
			queryFn: () => networkService.getNetworkForEnvironment(envId, networkId)
		});

		if (!network) {
			throw error(404, 'Network not found');
		}

		return {
			network
		};
	} catch (err: any) {
		console.error('Failed to load network:', err);
		if (err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load network details');
	}
};
