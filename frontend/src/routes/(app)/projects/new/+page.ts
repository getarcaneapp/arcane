import { loadTemplateAuthoringData, loadTemplateContent } from '$lib/utils/template-load';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url, parent }) => {
	const { queryClient } = await parent();

	const templateId = url.searchParams.get('templateId');
	const { defaultTemplates, templates: allTemplates, globalVariables } = await loadTemplateAuthoringData(parent);

	const selectedTemplate = templateId
		? await loadTemplateContent(queryClient as Parameters<typeof loadTemplateContent>[0], templateId)
		: null;

	return {
		composeTemplates: allTemplates,
		envTemplate: selectedTemplate?.envContent || defaultTemplates.envTemplate,
		defaultTemplate: selectedTemplate?.content || defaultTemplates.composeTemplate,
		selectedTemplate: selectedTemplate?.template || null,
		globalVariables
	};
};
