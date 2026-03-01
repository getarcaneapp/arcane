import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { containerService } from '$lib/services/container-service';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';
import { settingsService } from '$lib/services/settings-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const containerRequestOptions = resolveInitialTableRequest('arcane-container-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'created', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);

	// containers includes counts, settings is separate
	let containers;
	let settings;
	try {
		[containers, settings] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.containers.list(envId, containerRequestOptions),
				queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.settings.byEnvironment(envId),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load containers');
	}

	return {
		containers,
		containerRequestOptions,
		settings,
		// Use counts from the containers response
		containerStatusCounts: containers.counts ?? { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 }
	};
};
