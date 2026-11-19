import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { swarmEnvironmentsQuery } from '#lib/query/swarm.js';
import { swarmStackSourceQuery } from '#lib/query/swarm.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { extractApiErrorStatus, throwPageLoadError } from '#lib/utils/api.js';
import { destinationEnvironmentId, environmentName, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { loadTemplateAuthoringData, loadTemplateContent, type SelectedTemplateLoad } from '#lib/utils/template-load.js';
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
		environments = (await queryClient.query(swarmEnvironmentsQuery('swarm-stacks:create'))).managers;
		envId = destinationEnvironmentId(url.searchParams.get('env') ?? '', 'swarm-stacks:create', environments);
	}

	const loadSourceStack = async () => {
		if (!sourceStackName) {
			return null;
		}
		const result = await tryCatch(
			queryClient.query({ ...swarmStackSourceQuery(envId, sourceStackName), staleTime: 0, retry: false })
		);
		if (result.error === null) {
			return result.data;
		}
		if (extractApiErrorStatus(result.error) === 404) {
			throw error(404, m.saved_source_not_found());
		}
		throwPageLoadError(result.error, m.failed_to_load_saved_stack_source());
	};

	const [{ defaultTemplates, templates: allTemplates, globalVariables, permissions, loadErrors }, sourceStack] =
		await Promise.all([loadTemplateAuthoringData(parent), loadSourceStack()]);

	let selectedTemplate: SelectedTemplateLoad | null = null;
	if (templateId) {
		selectedTemplate = await loadTemplateContent(queryClient, templateId, permissions.canReadTemplates);
	}

	let envTemplate = selectedTemplate?.content?.envContent ?? defaultTemplates.swarmStackEnvTemplate;
	let defaultTemplate = selectedTemplate?.content?.content ?? defaultTemplates.swarmStackTemplate;
	if (isEditMode) {
		envTemplate = sourceStack?.envContent ?? '';
		defaultTemplate = sourceStack?.composeContent ?? '';
	}

	return {
		envId,
		environments,
		composeTemplates: allTemplates,
		envTemplate,
		defaultTemplate,
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
