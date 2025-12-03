import { templateService } from '$lib/services/template-service';
import type { Template } from '$lib/types/template.type';

export const load = async (): Promise<{ composeTemplate: string; envTemplate: string; templates: Template[] }> => {
	const [defaultTemplates, templates] = await Promise.all([
		templateService.getDefaultTemplates().catch((err) => {
			console.warn('Failed to load default templates:', err);
			return { composeTemplate: '', envTemplate: '' };
		}),
		templateService.getAllTemplates().catch((err) => {
			console.warn('Failed to load templates:', err);
			return [];
		})
	]);

	return {
		composeTemplate: defaultTemplates.composeTemplate,
		envTemplate: defaultTemplates.envTemplate,
		templates
	};
};
