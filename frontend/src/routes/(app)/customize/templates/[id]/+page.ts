import { tryCatch } from '#lib/utils/try-catch.js';
import { templateService } from '#lib/services/template-service.js';
import { variableService } from '#lib/services/variable-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { error } from '@sveltejs/kit';
import type { Template, TemplateContentData } from '#lib/types/swarm.js';
import type { GlobalVariable } from '#lib/types/variable.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	params,
	parent
}): Promise<{
	templateData: TemplateContentData;
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
				tryCatch(
					queryClient.query({
						queryKey: queryKeys.variables.list(),
						queryFn: () => variableService.list()
					})
				).then((result) => (result.error ? ([] as GlobalVariable[]) : result.data))
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
