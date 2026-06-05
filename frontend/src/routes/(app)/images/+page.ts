import { imageService } from '$lib/services/image-service';
import { settingsService } from '$lib/services/settings-service';
import { queryKeys } from '$lib/query/query-keys';
import { resolveListPageLoadContext } from '$lib/utils/tables';
import { throwPageLoadError } from '$lib/utils/api';
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

	return { images, imageRequestOptions, settings, imageUsageCounts };
};
