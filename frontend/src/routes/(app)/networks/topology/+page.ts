import { tryCatch } from '#lib/utils/try-catch.js';
import { networkService } from '#lib/services/network-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import type { PageLoad } from './$types';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	let topology;
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.networks.topology(envId),
				queryFn: () => networkService.getNetworkTopology(envId)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load network topology');
	} else {
		topology = operationResult.data;
	}

	return {
		envId,
		topology
	};
};
