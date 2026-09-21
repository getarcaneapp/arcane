import { parse } from 'yaml';
import { containerService } from '#lib/services/container-service.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';
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

	const [{ defaultTemplates, templates: allTemplates, globalVariables, permissions, loadErrors }, generated] = await Promise.all([
		loadTemplateAuthoringData(parent),
		sourceContainerIds.length ? generateFromContainers(sourceContainerIds, sourceEnvironmentId) : null
	]);

	const selectedTemplate = templateId
		? await loadTemplateContent(
				queryClient as Parameters<typeof loadTemplateContent>[0],
				templateId,
				permissions.canReadTemplates
			)
		: null;

	return {
		composeTemplates: allTemplates,
		envTemplate: generated ? '' : selectedTemplate?.content?.envContent || defaultTemplates.envTemplate,
		defaultTemplate: generated?.composeContent || selectedTemplate?.content?.content || defaultTemplates.composeTemplate,
		selectedTemplate: selectedTemplate?.content?.template || null,
		selectedTemplateError: selectedTemplate?.error ?? null,
		selectedTemplateForbidden: selectedTemplate?.forbidden ?? false,
		templatePermissions: permissions,
		templateLoadErrors: loadErrors,
		sourceContainerIds,
		sourceEnvironmentId,
		sourceContainerName: generated?.name ?? '',
		globalVariables
	};
};
