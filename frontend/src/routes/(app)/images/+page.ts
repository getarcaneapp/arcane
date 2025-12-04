import { imageService } from '$lib/services/image-service';
import { settingsService } from '$lib/services/settings-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const imageRequestOptions = resolveInitialTableRequest('arcane-image-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'created',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);
	const [images, settings, imageUsageCounts] = await Promise.all([
		imageService.getImages(imageRequestOptions),
		settingsService.getSettings(),
		imageService.getImageUsageCounts()
	]);

	return { images, imageRequestOptions, settings, imageUsageCounts };
};
