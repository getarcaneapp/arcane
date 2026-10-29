import { m } from '#lib/paraglide/messages.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { containerRegistryService } from '#lib/services/container-registry-service.js';
import type { RegistryRepository, RegistryTag } from '#lib/types/docker.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { extractApiErrorMessage, throwPageLoadError } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

const catalogPageSize = 500;
const tagsPageSize = 50;

export const load: PageLoad = async ({ params, parent, url }) => {
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

	const repository = url.searchParams.get('repository')?.trim() ?? '';
	const search = url.searchParams.get('search')?.trim() ?? '';
	const page = Math.max(1, Number(url.searchParams.get('page')) || 1);

	// The catalog is only needed for the repository grid. Registries without a
	// catalog API fail here, and the page offers opening a repository by name instead.
	let catalog: Paginated<RegistryRepository> | null = null;
	let catalogError: string | null = null;
	if (!repository) {
		const catalogRequest: SearchPaginationSortRequest = {
			search,
			pagination: { page: 1, limit: catalogPageSize },
			sort: { column: 'name', direction: 'asc' }
		};
		const catalogResult = await tryCatch(
			queryClient.query({
				queryKey: queryKeys.containerRegistries.repositories(params.id, catalogRequest),
				queryFn: () => containerRegistryService.getRepositories(params.id, catalogRequest),
				staleTime: 30_000
			})
		);
		if (catalogResult.error === null) {
			catalog = catalogResult.data;
		} else {
			catalogError = extractApiErrorMessage(catalogResult.error);
		}
	}
	const tagsRequest: SearchPaginationSortRequest = {
		search,
		pagination: { page, limit: tagsPageSize },
		sort: { column: 'name', direction: 'asc' }
	};

	let tags: Paginated<RegistryTag> = {
		data: [],
		pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: tagsPageSize }
	};
	let tagsError: string | null = null;
	if (repository) {
		const tagsResult = await tryCatch(
			queryClient.query({
				queryKey: queryKeys.containerRegistries.tags(params.id, repository, tagsRequest),
				queryFn: () => containerRegistryService.getTags(params.id, repository, tagsRequest)
			})
		);
		if (tagsResult.error === null) {
			tags = tagsResult.data;
		} else {
			tagsError = extractApiErrorMessage(tagsResult.error);
		}
	}

	return {
		registry: registryResult.data,
		catalog,
		catalogError,
		repository,
		search,
		tags,
		tagsError
	};
};
