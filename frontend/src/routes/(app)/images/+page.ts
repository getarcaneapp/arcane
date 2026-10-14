import { queryKeys } from '#lib/query/query-keys.js';
import { imageService } from '#lib/services/image-service.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';
import { resolveListPageLoadContext } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const {
		queryClient,
		environments,
		scope,
		requestOptions: imageRequestOptions
	} = await resolveListPageLoadContext(parent, 'arcane-image-table', {
		column: 'created',
		direction: 'desc'
	});
	await Promise.all(environments.map((env) => featureStore.load(env.id)));
	let images;
	let imageUsageCounts;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.images.list(scope, imageRequestOptions),
					queryFn: () =>
						fetchAcrossEnvironments(environments, imageRequestOptions, (id, request, config) =>
							imageService.getImagesForEnvironment(id, request, config)
						)
				}),
				queryClient.query({
					queryKey: queryKeys.images.usageCounts(scope),
					queryFn: () =>
						sumAcrossEnvironments(environments, (id, config) => imageService.getImageUsageCountsForEnvironment(id, config))
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load images');
	} else {
		[images, imageUsageCounts] = operationResult.data;
	}

	return { scope, images, imageRequestOptions, imageUsageCounts };
};
