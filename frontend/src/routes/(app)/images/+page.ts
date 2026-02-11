import { imageService } from '$lib/services/image-service';
import { settingsService } from '$lib/services/settings-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

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

	return { images, imageRequestOptions, settings, imageUsageCounts };
};
