import type { QueryClient } from '@tanstack/svelte-query';

import { projectWorkspaceFolderQuery } from '#lib/query/projects.js';
import { projectDetailQuery } from '#lib/query/projects.js';
import { variableListQuery } from '#lib/query/variables.js';
import { extractApiErrorMessage, throwPageLoadError } from '#lib/utils/api.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

async function loadGlobalVariables(queryClient: QueryClient, canList: boolean) {
	if (!canList) return { globalVariables: [], globalVariablesError: null };
	const result = await tryCatch(queryClient.query(variableListQuery()));
	if (result.error !== null) return { globalVariables: [], globalVariablesError: extractApiErrorMessage(result.error) };
	return { globalVariables: result.data, globalVariablesError: null };
}

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient, user } = await parent();
	const envId = await resolveDetailEnvironmentId(url);

	const [projectLoad, { globalVariables, globalVariablesError }] = await Promise.all([
		tryCatch(queryClient.query(projectDetailQuery(envId, params.projectId))),
		loadGlobalVariables(queryClient, userHasPermission(user, 'variables:list'))
	]);
	if (projectLoad.error !== null) {
		throwPageLoadError(projectLoad.error, 'Failed to load project');
	}
	const project = projectLoad.data;

	void queryClient.prefetchQuery(projectWorkspaceFolderQuery(envId, params.projectId, ''));

	const editorState = {
		name: project.name || '',
		composeContent: project.composeContent || '',
		envContent: project.envContent || '',
		overrideContent: project.overrideContent || '',
		originalName: project.name || '',
		originalComposeContent: project.composeContent || '',
		originalEnvContent: project.envContent || '',
		originalOverrideContent: project.overrideContent || ''
	};

	return {
		envId,
		projectId: params.projectId,
		project,
		editorState,
		globalVariables,
		globalVariablesError,
		error: null as string | null
	};
};
