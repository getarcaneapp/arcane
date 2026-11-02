import { fetchEnvironmentTopology } from '#lib/components/network-diagram/topology.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	await environmentStore.ready;
	const scope = environmentStore.shownKey;

	let topology;
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.networks.topology(scope),
				queryFn: fetchEnvironmentTopology
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load network topology');
	} else {
		topology = operationResult.data;
	}

	return {
		scope,
		topology
	};
};
