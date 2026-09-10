import { tryCatch } from '#lib/utils/try-catch.js';
import { imageService } from '#lib/services/image-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { resolveListPageLoadContext } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const {
		queryClient,
		envId,
		requestOptions: imageRequestOptions
	} = await resolveListPageLoadContext(parent, 'arcane-image-table', {
		column: 'created',
		direction: 'desc'
	});
	let images;
	let settings;
	let imageUsageCounts;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.images.list(envId, imageRequestOptions),
					queryFn: () => imageService.getImagesForEnvironment(envId, imageRequestOptions)
				}),
				queryClient.query({
					queryKey: queryKeys.settings.byEnvironment(envId),
					queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
				}),
				queryClient.query({
					queryKey: queryKeys.images.usageCounts(envId),
					queryFn: () => imageService.getImageUsageCountsForEnvironment(envId)
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load images');
	} else {
		[images, settings, imageUsageCounts] = operationResult.data;
	}

	return { envId, images, imageRequestOptions, settings, imageUsageCounts };
};
