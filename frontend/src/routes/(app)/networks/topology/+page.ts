import { networkTopologyQuery } from '#lib/query/networks.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	await environmentStore.ready;
	const scope = environmentStore.shownKey;

	const topology = await tryCatch(queryClient.query(networkTopologyQuery(scope)));
	if (topology.error !== null) {
		throwPageLoadError(topology.error, 'Failed to load network topology');
	}

	return {
		scope,
		topology: topology.data
	};
};
