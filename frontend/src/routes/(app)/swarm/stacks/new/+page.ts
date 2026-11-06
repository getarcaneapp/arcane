import { error } from '@sveltejs/kit';

import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { extractApiErrorStatus, throwPageLoadError } from '#lib/utils/api.js';
import { destinationEnvironmentId, environmentName, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { loadTemplateAuthoringData, loadTemplateContent } from '#lib/utils/template-load.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, url }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const templateId = url.searchParams.get('templateId');
	const fromStack = url.searchParams.get('fromStack');
	const sourceStackName = fromStack ? decodeURIComponent(fromStack) : null;
	const isEditMode = sourceStackName !== null;

	let envId: string;
	let environments: EnvironmentRef[];
	if (isEditMode) {
		envId = await resolveDetailEnvironmentId(url);
		environments = [{ id: envId, name: environmentName(envId) }];
	} else {
		environments = (await queryClient.query(swarmEnvironmentsQueryOptions('swarm-stacks:create'))).managers;
		envId = destinationEnvironmentId(url.searchParams.get('env') ?? '', 'swarm-stacks:create', environments);
	}

	const sourceStackPromise = sourceStackName
		? tryCatch(swarmService.getStackSource(sourceStackName, envId)).then((result) => {
				if (result.error === null) return result.data;
				if (extractApiErrorStatus(result.error) === 404) throw error(404, m.saved_source_not_found());
				throwPageLoadError(result.error, m.failed_to_load_saved_stack_source());
			})
		: Promise.resolve(null);

	const [{ defaultTemplates, templates: allTemplates, globalVariables, permissions, loadErrors }, sourceStack] =
		await Promise.all([loadTemplateAuthoringData(parent), sourceStackPromise]);

	const selectedTemplate = templateId
		? await loadTemplateContent(
				queryClient as Parameters<typeof loadTemplateContent>[0],
				templateId,
				permissions.canReadTemplates
			)
		: null;

	return {
		envId,
		environments,
		composeTemplates: allTemplates,
		envTemplate: isEditMode
			? (sourceStack?.envContent ?? '')
			: (selectedTemplate?.content?.envContent ?? defaultTemplates.swarmStackEnvTemplate),
		defaultTemplate: isEditMode
			? (sourceStack?.composeContent ?? '')
			: (selectedTemplate?.content?.content ?? defaultTemplates.swarmStackTemplate),
		overrideTemplate: sourceStack?.overrideContent ?? '',
		sourceFiles: sourceStack?.files ?? [],
		isEditMode,
		selectedTemplate: selectedTemplate?.content?.template || null,
		selectedTemplateError: selectedTemplate?.error ?? null,
		selectedTemplateForbidden: selectedTemplate?.forbidden ?? false,
		templateLoadErrors: loadErrors,
		sourceStackName: sourceStack?.name || sourceStackName || null,
		globalVariables
	};
};
