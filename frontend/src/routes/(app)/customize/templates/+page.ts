import { templateListQuery, templateRegistriesQuery } from '#lib/query/templates.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import type { Template, TemplateRegistry } from '#lib/types/swarm.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	parent
}): Promise<{
	templates: Paginated<Template>;
	registries: TemplateRegistry[];
	templateRequestOptions: SearchPaginationSortRequest;
}> => {
	const { queryClient } = await parent();

	const templateRequestOptions = resolveInitialTableRequest('arcane-template-gallery', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'name', direction: 'asc' }
	} satisfies SearchPaginationSortRequest);

	const [templates, registries] = await Promise.all([
		tryCatch(queryClient.query(templateListQuery(templateRequestOptions))).then((result) =>
			result.error
				? {
						data: [],
						pagination: { currentPage: 1, totalPages: 0, totalItems: 0, itemsPerPage: 20 }
					}
				: result.data
		),
		tryCatch(queryClient.query(templateRegistriesQuery())).then((result) => (result.error ? [] : result.data))
	]);

	return {
		templates,
		registries,
		templateRequestOptions
	};
};
