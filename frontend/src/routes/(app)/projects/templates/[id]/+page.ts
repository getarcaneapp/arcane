import { error } from '@sveltejs/kit';

import { queryKeys } from '#lib/query/query-keys.js';
import { templateService } from '#lib/services/template-service.js';
import type { GlobalVariable } from '#lib/types/env.js';
import type { Template, TemplateContent } from '#lib/types/template.js';
import { loadGlobalVariablesOrEmpty } from '#lib/utils/template-load.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	params,
	parent
}): Promise<{
	templateData: TemplateContent;
	allTemplates: Template[];
	globalVariables: GlobalVariable[];
}> => {
	const { queryClient } = await parent();

	const operationResult = await tryCatch(
		(async () => {
			const [templateData, allTemplates, globalVariables] = await Promise.all([
				queryClient.query({
					queryKey: queryKeys.templates.content(params.id),
					queryFn: () => templateService.getTemplateContent(params.id)
				}),
				queryClient.query({
					queryKey: queryKeys.templates.allTemplates(),
					queryFn: () => templateService.getAllTemplates()
				}),
				loadGlobalVariablesOrEmpty(queryClient)
			]);

			return {
				templateData,
				allTemplates,
				globalVariables
			};
		})()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		console.error('Failed to load template:', err);
		throw error(404, 'Template not found');
	} else {
		return operationResult.data;
	}
};
