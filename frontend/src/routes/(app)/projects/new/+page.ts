import { parse } from 'yaml';
import { containerService } from '#lib/services/container-service.js';
import { throwPageLoadError, tryCatch } from '#lib/utils/api.js';
import { loadTemplateAuthoringData, loadTemplateContent } from '#lib/utils/template-load.js';
import type { PageLoad } from './$types';

async function generateFromContainers(ids: string[], environmentId?: string) {
	const { data, error } = await tryCatch(containerService.generateCompose(ids, environmentId));
	if (error) throwPageLoadError(error, 'Failed to generate compose file from containers');

	const parsed = await tryCatch(Promise.resolve(data.composeContent).then(parse));
	return { composeContent: data.composeContent, name: Object.keys(parsed.data?.services ?? {})[0] ?? '' };
}

export const load: PageLoad = async ({ url, parent }) => {
	const { queryClient } = await parent();

	const templateId = url.searchParams.get('templateId');
	const sourceContainerIds = url.searchParams.get('fromContainers')?.split(',').filter(Boolean) ?? [];
	const sourceEnvironmentId = url.searchParams.get('fromEnv') || undefined;

	const [{ defaultTemplates, templates: allTemplates, globalVariables }, generated] = await Promise.all([
		loadTemplateAuthoringData(parent),
		sourceContainerIds.length ? generateFromContainers(sourceContainerIds, sourceEnvironmentId) : null
	]);

	const selectedTemplate = templateId
		? await loadTemplateContent(queryClient as Parameters<typeof loadTemplateContent>[0], templateId)
		: null;

	return {
		composeTemplates: allTemplates,
		envTemplate: generated ? '' : selectedTemplate?.envContent || defaultTemplates.envTemplate,
		defaultTemplate: generated?.composeContent || selectedTemplate?.content || defaultTemplates.composeTemplate,
		selectedTemplate: selectedTemplate?.template || null,
		sourceContainerIds,
		sourceEnvironmentId,
		sourceContainerName: generated?.name ?? '',
		globalVariables
	};
};
