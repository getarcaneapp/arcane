import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const result = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.podman.pod(envId, params.id),
			queryFn: () => podmanService.getPod(params.id)
		})
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, 'Failed to load pod');
	}
	return { envId, pod: result.data };
};
