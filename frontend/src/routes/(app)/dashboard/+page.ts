import { containerService } from '$lib/services/container-service';
import { imageService } from '$lib/services/image-service';
import { settingsService } from '$lib/services/settings-service';
import { systemService } from '$lib/services/system-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

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

	const [containers, images, containerStatusCounts] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.containers.list(envId, containerRequestOptions),
			queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.images.list(envId, imageRequestOptions),
			queryFn: () => imageService.getImagesForEnvironment(envId, imageRequestOptions)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.containers.statusCounts(envId),
			queryFn: () => containerService.getContainerStatusCountsForEnvironment(envId)
		})
	]);

	const [dockerInfoResult, settingsResult] = await Promise.allSettled([
		queryClient.fetchQuery({
			queryKey: queryKeys.system.dockerInfo(envId),
			queryFn: () => systemService.getDockerInfoForEnvironment(envId)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.settings.byEnvironment(envId),
			queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
		})
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
