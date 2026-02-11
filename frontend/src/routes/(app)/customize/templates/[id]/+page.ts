import { templateService } from '$lib/services/template-service';
import { queryKeys } from '$lib/query/query-keys';
import { error } from '@sveltejs/kit';
import type { Template, TemplateContentData } from '$lib/types/template.type';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	params,
	parent
}): Promise<{
	templateData: TemplateContentData;
	allTemplates: Template[];
}> => {
	const { queryClient } = await parent();

	try {
		const [templateData, allTemplates] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.templates.content(params.id),
				queryFn: () => templateService.getTemplateContent(params.id)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
		]);

		return {
			templateData,
			allTemplates
		};
	} catch (err) {
		console.error('Failed to load template:', err);
		throw error(404, 'Template not found');
	}
};
