import { volumeService } from '$lib/services/volume-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const volumeRequestOptions = resolveInitialTableRequest('arcane-volumes-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const [volumes, volumeUsageCounts] = await Promise.all([
		volumeService.getVolumes(volumeRequestOptions),
		volumeService.getVolumeUsageCounts()
	]);

	return { volumes, volumeRequestOptions, volumeUsageCounts };
};
