import { templateService } from '$lib/services/template-service';
import type { Template, TemplateRegistry } from '$lib/types/template.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';

export const load = async (): Promise<{
	templates: Paginated<Template>;
	registries: TemplateRegistry[];
	templateRequestOptions: SearchPaginationSortRequest;
}> => {
	const templateRequestOptions = resolveInitialTableRequest('arcane-template-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'name', direction: 'asc' }
	} satisfies SearchPaginationSortRequest);

	const [templates, registries] = await Promise.all([
		templateService.getTemplates(templateRequestOptions).catch(() => ({
			data: [],
			pagination: { currentPage: 1, totalPages: 0, totalItems: 0, itemsPerPage: 20 }
		})),
		templateService.getRegistries().catch(() => [])
	]);

	return {
		templates,
		registries,
		templateRequestOptions
	};
};
