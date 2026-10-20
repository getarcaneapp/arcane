import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { podmanService } from '#lib/services/podman-service.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);
	const result = await tryCatch(
		Promise.all([
			queryClient.query({
				queryKey: queryKeys.podman.quadlet(envId, params.name),
				queryFn: () => podmanService.getQuadlet(params.name, envId)
			}),
			queryClient.query({
				queryKey: queryKeys.podman.quadletFile(envId, params.name),
				queryFn: () => podmanService.getQuadletFile(params.name, envId)
			})
		])
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.podman_quadlet() }));
	}
	const [quadlet, file] = result.data;
	return { envId, quadlet, file };
};
