import { tryCatch } from '#lib/utils/try-catch.js';
import { containerRegistryService } from '#lib/services/container-registry-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
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

	const registries = await queryClient.query({
		queryKey: queryKeys.containerRegistries.list(registryRequestOptions),
		queryFn: () => containerRegistryService.getRegistries(registryRequestOptions)
	});
	const pullUsage = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.containerRegistries.pullUsage(),
			queryFn: () => containerRegistryService.getPullUsage()
		})
	).then((result) => (result.error ? null : result.data));

	return { registries, registryRequestOptions, pullUsage };
};
