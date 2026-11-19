import { m } from '#lib/paraglide/messages.js';
import { podmanQuadletFileQuery, podmanQuadletQuery } from '#lib/query/podman.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);
	const result = await tryCatch(
		Promise.all([
			queryClient.query(podmanQuadletQuery(envId, params.name)),
			queryClient.query(podmanQuadletFileQuery(envId, params.name))
		])
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.podman_quadlet() }));
	}
	const [quadlet, file] = result.data;
	return { envId, quadlet, file };
};
