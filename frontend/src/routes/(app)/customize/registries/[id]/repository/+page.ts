import { error } from '@sveltejs/kit';
import { tryCatch } from '#lib/utils/try-catch.js';
import { containerRegistryService } from '#lib/services/container-registry-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { m } from '#lib/paraglide/messages.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const repository = url.searchParams.get('name')?.trim() ?? '';
	if (!repository) {
		error(400, m.images_tag_repository_required());
	}

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

	const requestOptions = resolveInitialTableRequest('arcane-registry-tags-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const tagsResult = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.containerRegistries.tags(params.id, repository, requestOptions),
			queryFn: () => containerRegistryService.getTags(params.id, repository, requestOptions)
		})
	);
	if (tagsResult.error !== null) {
		throwPageLoadError(tagsResult.error, m.registries_tags_load_failed({ repository }));
	}

	return {
		registry: registryResult.data,
		repository,
		requestOptions,
		tags: tagsResult.data
	};
};
