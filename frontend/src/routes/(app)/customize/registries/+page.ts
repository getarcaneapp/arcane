import { containerRegistryService } from '$lib/services/container-registry-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const registryRequestOptions = resolveInitialTableRequest('arcane-registries-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'url',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const registries = await containerRegistryService.getRegistries(registryRequestOptions);

	return { registries, registryRequestOptions };
};
