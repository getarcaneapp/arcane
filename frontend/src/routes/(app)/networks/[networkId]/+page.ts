import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { networkService } from '$lib/services/network-service';

export const load: PageLoad = async ({ params, url }) => {
	const { networkId } = params;
	const sort = url.searchParams.get('sort') || 'name';
	const order = url.searchParams.get('order') || 'asc';

	try {
		const network = await networkService.getNetwork(networkId, sort, order);

		if (!network) {
			throw error(404, 'Network not found');
		}

		return {
			network,
			sort,
			order
		};
	} catch (err: any) {
		console.error('Failed to load network:', err);
		if (err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load network details');
	}
};
