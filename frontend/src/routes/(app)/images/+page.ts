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
	try {
		[images, settings, imageUsageCounts] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.images.list(envId, imageRequestOptions),
				queryFn: () => imageService.getImagesForEnvironment(envId, imageRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.settings.byEnvironment(envId),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.images.usageCounts(envId),
				queryFn: () => imageService.getImageUsageCountsForEnvironment(envId)
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load images');
	}

	return { envId, images, imageRequestOptions, settings, imageUsageCounts };
};
