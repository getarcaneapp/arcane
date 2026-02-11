import { containerRegistryService } from '$lib/services/container-registry-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

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

	const registries = await queryClient.fetchQuery({
		queryKey: queryKeys.containerRegistries.list(registryRequestOptions),
		queryFn: () => containerRegistryService.getRegistries(registryRequestOptions)
	});

	return { registries, registryRequestOptions };
};
