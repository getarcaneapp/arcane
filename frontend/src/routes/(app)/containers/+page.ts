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

	const [containers, containerStatusCounts, settings] = await Promise.all([
		containerService.getContainers(containerRequestOptions),
		containerService.getContainerStatusCounts(),
		settingsService.getSettings()
	]);

	return { containers, containerStatusCounts, containerRequestOptions, settings };
};
