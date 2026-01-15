import { templateService } from '$lib/services/template-service';

export const load = async ({ url }) => {
	const templateId = url.searchParams.get('templateId');

	const [allTemplates, defaultTemplates, selectedTemplate] = await Promise.all([
		templateService.getAllTemplates().catch((err) => {
			console.warn('Failed to load templates:', err);
			return [];
		}),
		templateService.getDefaultTemplates().catch((err) => {
			console.warn('Failed to load default templates:', err);
			return { composeTemplate: '', envTemplate: '' };
		}),
		templateId
			? templateService.getTemplateContent(templateId).catch((err) => {
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
