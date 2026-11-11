import { error } from '@sveltejs/kit';

import { containerDetailQuery } from '#lib/query/containers.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);
	const containerId = params.containerId;

	const containerResult = await tryCatch(queryClient.query(containerDetailQuery(envId, containerId)));
	if (containerResult.error !== null) {
		throwPageLoadError(containerResult.error, 'Failed to load container details');
	}
	const container = containerResult.data;
	if (!container) {
		error(404, 'Container not found');
	}

	return {
		envId,
		container
	};
};
