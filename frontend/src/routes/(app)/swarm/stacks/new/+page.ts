import { templateService } from '$lib/services/template-service';
import { swarmService } from '$lib/services/swarm-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url }) => {
	const templateId = url.searchParams.get('templateId');
	const fromStack = url.searchParams.get('fromStack');
	const sourceStackName = fromStack ? decodeURIComponent(fromStack) : null;

	const [allTemplates, defaultTemplates, selectedTemplate, sourceStack, globalVariables] = await Promise.all([
		templateService.getAllTemplates().catch((err) => {
			console.warn('Failed to load templates:', err);
			return [];
		}),
		templateService.getDefaultTemplates().catch((err) => {
			console.warn('Failed to load default templates:', err);
			return { composeTemplate: '', swarmStackTemplate: '', swarmStackEnvTemplate: '', envTemplate: '' };
		}),
		templateId
			? templateService.getTemplateContent(templateId).catch((err) => {
					console.warn('Failed to load selected template:', err);
					return null;
				})
			: Promise.resolve(null),
		sourceStackName
			? swarmService.getStackSource(sourceStackName).catch((err) => {
					console.warn('Failed to load source stack content:', err);
					return null;
				})
			: Promise.resolve(null),
		templateService.getGlobalVariables().catch((err) => {
			console.warn('Failed to load global variables:', err);
			return [];
		})
	]);

	return {
		composeTemplates: allTemplates,
		envTemplate: selectedTemplate?.envContent ?? sourceStack?.envContent ?? defaultTemplates.swarmStackEnvTemplate,
		defaultTemplate: selectedTemplate?.content ?? sourceStack?.composeContent ?? defaultTemplates.swarmStackTemplate,
		selectedTemplate: selectedTemplate?.template || null,
		sourceStackName: sourceStack?.name || sourceStackName || null,
		globalVariables
	};
};
