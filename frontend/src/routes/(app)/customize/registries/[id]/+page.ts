import { tryCatch } from '#lib/utils/try-catch.js';
import { containerRegistryService } from '#lib/services/container-registry-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { extractApiErrorMessage, throwPageLoadError } from '#lib/utils/api.js';
import { m } from '#lib/paraglide/messages.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();

	const registryResult = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.containerRegistries.detail(params.id),
			queryFn: () => containerRegistryService.getRegistry(params.id)
		})
	);
	if (registryResult.error !== null) {
		throwPageLoadError(registryResult.error, m.common_load_failed({ resource: m.resource_registry() }));
	}

	const requestOptions = resolveInitialTableRequest('arcane-registry-repositories-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	// Registries without a catalog API fail here; the page offers opening a repository by name instead.
	const repositoriesResult = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.containerRegistries.repositories(params.id, requestOptions),
			queryFn: () => containerRegistryService.getRepositories(params.id, requestOptions)
		})
	);

	return {
		registry: registryResult.data,
		requestOptions,
		repositories: repositoriesResult.data,
		repositoriesError: repositoriesResult.error === null ? null : extractApiErrorMessage(repositoriesResult.error)
	};
};
