import { error } from '@sveltejs/kit';
import { templateService } from '$lib/services/template-service';
import { swarmService } from '$lib/services/swarm-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ url }) => {
	const templateId = url.searchParams.get('templateId');
	const fromStack = url.searchParams.get('fromStack');
	const sourceStackName = fromStack ? decodeURIComponent(fromStack) : null;
	const isEditMode = sourceStackName !== null;

	const sourceStackPromise = sourceStackName
		? swarmService.getStackSource(sourceStackName).catch((err: any) => {
				console.warn('Failed to load source stack content:', err);
				if (err?.status === 404) {
					throw error(404, 'Saved source not found');
				}
				throw error(err?.status || 500, err?.message || 'Failed to load saved stack source');
			})
		: Promise.resolve(null);

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
		sourceStackPromise,
		templateService.getGlobalVariables().catch((err) => {
			console.warn('Failed to load global variables:', err);
			return [];
		})
	]);

	return {
		composeTemplates: allTemplates,
		envTemplate: isEditMode
			? (sourceStack?.envContent ?? '')
			: (selectedTemplate?.envContent ?? defaultTemplates.swarmStackEnvTemplate),
		defaultTemplate: isEditMode
			? (sourceStack?.composeContent ?? '')
			: (selectedTemplate?.content ?? defaultTemplates.swarmStackTemplate),
		isEditMode,
		selectedTemplate: selectedTemplate?.template || null,
		sourceStackName: sourceStack?.name || sourceStackName || null,
		globalVariables
	};
};
