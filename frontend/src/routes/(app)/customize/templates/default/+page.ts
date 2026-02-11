import { templateService } from '$lib/services/template-service';
import { queryKeys } from '$lib/query/query-keys';
import type { Template } from '$lib/types/template.type';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	parent
}): Promise<{ composeTemplate: string; envTemplate: string; templates: Template[] }> => {
	const { queryClient } = await parent();

	const [defaultTemplates, templates] = await Promise.all([
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.defaults(),
				queryFn: () => templateService.getDefaultTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load default templates:', err);
				return { composeTemplate: '', envTemplate: '' };
			}),
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
			.catch((err) => {
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
