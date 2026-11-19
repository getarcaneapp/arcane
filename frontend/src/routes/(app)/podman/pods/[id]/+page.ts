import { m } from '#lib/paraglide/messages.js';
import { podmanPodQuery } from '#lib/query/podman.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);
	const result = await tryCatch(queryClient.query(podmanPodQuery(envId, params.id)));
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.podman_pod() }));
	}
	return { envId, pod: result.data };
};
