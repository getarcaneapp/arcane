import { imageListQuery, imageUsageCountsQuery } from '#lib/query/images.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;
	const environments = readableEnvironments('images:list');
	const imageRequestOptions = resolveInitialListPageRequest('arcane-image-table', {
		column: 'created',
		direction: 'desc'
	});

	const result = await tryCatch(
		Promise.all([
			queryClient.query(imageListQuery(scope, environments, imageRequestOptions)),
			queryClient.query(imageUsageCountsQuery(scope, environments)),
			...environments.map((env) => featureStore.load(env.id))
		])
	);
	if (result.error !== null) throwPageLoadError(result.error, 'Failed to load images');
	const [images, imageUsageCounts] = result.data;

	return { scope, images, imageRequestOptions, imageUsageCounts };
};
