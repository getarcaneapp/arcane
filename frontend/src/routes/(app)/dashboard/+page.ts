import { containerService } from '$lib/services/container-service';
import { imageService } from '$lib/services/image-service';
import { settingsService } from '$lib/services/settings-service';
import { systemService } from '$lib/services/system-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const containerRequestOptions: SearchPaginationSortRequest = {
			pagination: {
				page: 1,
				limit: 5
			},
			sort: {
				column: 'created',
				direction: 'desc' as const
			}
		},
		imageRequestOptions: SearchPaginationSortRequest = {
			pagination: {
				page: 1,
				limit: 5
			},
			sort: {
				column: 'size',
				direction: 'desc' as const
			}
		};

	const containers = await containerService.getContainers(containerRequestOptions);
	const images = await imageService.getImages(imageRequestOptions);
	const containerStatusCounts = await containerService.getContainerStatusCounts();

	const [dockerInfoResult, settingsResult] = await Promise.allSettled([
		systemService.getDockerInfo(),
		settingsService.getSettings()
	]);

	const dockerInfo = dockerInfoResult.status === 'fulfilled' ? dockerInfoResult.value : null;
	const settings = settingsResult.status === 'fulfilled' ? settingsResult.value : null;

	return {
		dockerInfo,
		containers,
		images,
		settings,
		containerRequestOptions,
		imageRequestOptions,
		containerStatusCounts
	};
};
