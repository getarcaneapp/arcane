import { templateService } from '$lib/services/template-service';
import { queryKeys } from '$lib/query/query-keys';
import type { Template, TemplateRegistry } from '$lib/types/template.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	parent
}): Promise<{
	templates: Paginated<Template>;
	registries: TemplateRegistry[];
	templateRequestOptions: SearchPaginationSortRequest;
}> => {
	const { queryClient } = await parent();

	const templateRequestOptions = resolveInitialTableRequest('arcane-template-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'name', direction: 'asc' }
	} satisfies SearchPaginationSortRequest);

	const [templates, registries] = await Promise.all([
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.list(templateRequestOptions),
				queryFn: () => templateService.getTemplates(templateRequestOptions)
			})
			.catch(() => ({
				data: [],
				pagination: { currentPage: 1, totalPages: 0, totalItems: 0, itemsPerPage: 20 }
			})),
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.registries(),
				queryFn: () => templateService.getRegistries()
			})
			.catch(() => [])
	]);

	return {
		templates,
		registries,
		templateRequestOptions
	};
};
