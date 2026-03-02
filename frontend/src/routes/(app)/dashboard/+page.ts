import { containerService } from '$lib/services/container-service';
import { dashboardService } from '$lib/services/dashboard-service';
import { imageService } from '$lib/services/image-service';
import { settingsService } from '$lib/services/settings-service';
import { systemService } from '$lib/services/system-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, url }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const debugAllGood = url.searchParams.get('debugAllGood') === 'true';

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

	let containers;
	let images;
	let containerStatusCounts;
	try {
		[containers, images, containerStatusCounts] = await Promise.all([
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
	} catch (err) {
		throwPageLoadError(err, 'Failed to load dashboard data');
	}

	const [dockerInfoResult, settingsResult, imageUsageCountsResult, dashboardActionItemsResult] = await Promise.allSettled([
		queryClient.fetchQuery({
			queryKey: queryKeys.system.dockerInfo(envId),
			queryFn: () => systemService.getDockerInfoForEnvironment(envId)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.settings.byEnvironment(envId),
			queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.images.usageCounts(envId),
			queryFn: () => imageService.getImageUsageCountsForEnvironment(envId)
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.dashboard.actionItems(envId, debugAllGood),
			queryFn: () => dashboardService.getActionItemsForEnvironment(envId, { debugAllGood })
		})
	]);

	const dockerInfo = dockerInfoResult.status === 'fulfilled' ? dockerInfoResult.value : null;
	const settings = settingsResult.status === 'fulfilled' ? settingsResult.value : null;
	const imageUsageCounts = imageUsageCountsResult.status === 'fulfilled' ? imageUsageCountsResult.value : null;
	const dashboardActionItems = dashboardActionItemsResult.status === 'fulfilled' ? dashboardActionItemsResult.value : null;

	return {
		dockerInfo,
		containers,
		images,
		settings,
		imageUsageCounts,
		dashboardActionItems,
		debugAllGood,
		containerRequestOptions,
		imageRequestOptions,
		containerStatusCounts
	};
};
