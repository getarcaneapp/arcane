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
		Promise.all([
			queryClient.query({
				queryKey: queryKeys.podman.quadlet(envId, params.name),
				queryFn: () => podmanService.getQuadlet(params.name)
			}),
			queryClient.query({
				queryKey: queryKeys.podman.quadletFile(envId, params.name),
				queryFn: () => podmanService.getQuadletFile(params.name)
			})
		])
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, 'Failed to load quadlet');
	}
	const [quadlet, file] = result.data;
	return { envId, quadlet, file };
};
