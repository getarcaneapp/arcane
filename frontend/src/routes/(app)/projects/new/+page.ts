import { templateService } from '$lib/services/template-service';
import { queryKeys } from '$lib/query/query-keys';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url, parent }) => {
	const { queryClient } = await parent();

	const templateId = url.searchParams.get('templateId');

	const [allTemplates, defaultTemplates, selectedTemplate] = await Promise.all([
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load templates:', err);
				return [];
			}),
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.defaults(),
				queryFn: () => templateService.getDefaultTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load default templates:', err);
				return { composeTemplate: '', envTemplate: '' };
			}),
		templateId
			? queryClient
					.fetchQuery({
						queryKey: queryKeys.templates.content(templateId),
						queryFn: () => templateService.getTemplateContent(templateId)
					})
					.catch((err) => {
						console.warn('Failed to load selected template:', err);
						return null;
					})
			: Promise.resolve(null)
	]);

	return {
		composeTemplates: allTemplates,
		envTemplate: selectedTemplate?.envContent || defaultTemplates.envTemplate,
		defaultTemplate: selectedTemplate?.content || defaultTemplates.composeTemplate,
		selectedTemplate: selectedTemplate?.template || null
	};
};
