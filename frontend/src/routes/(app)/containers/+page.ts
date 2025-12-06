import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { containerService } from '$lib/services/container-service';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';
import { settingsService } from '$lib/services/settings-service';

export const load: PageLoad = async () => {
	const containerRequestOptions = resolveInitialTableRequest('arcane-container-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'created', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);

	// containers includes counts, settings is separate
	const [containers, settings] = await Promise.all([
		containerService.getContainers(containerRequestOptions),
		settingsService.getSettings()
	]);

	return {
		containers,
		containerRequestOptions,
		settings,
		// Use counts from the containers response
		containerStatusCounts: containers.counts ?? { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 }
	};
};
