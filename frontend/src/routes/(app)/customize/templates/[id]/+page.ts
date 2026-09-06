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

	try {
		const [templateData, allTemplates, globalVariables] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.templates.content(params.id),
				queryFn: () => templateService.getTemplateContent(params.id)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			}),
			queryClient
				.fetchQuery({
					queryKey: queryKeys.variables.list(),
					queryFn: () => variableService.list()
				})
				.catch(() => [] as GlobalVariable[])
		]);

		return {
			templateData,
			allTemplates,
			globalVariables
		};
	} catch (err) {
		console.error('Failed to load template:', err);
		throw error(404, 'Template not found');
	}
};
