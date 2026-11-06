import type { QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { projectService } from '#lib/services/project-service.js';
import { projectWorkspaceService } from '#lib/services/project-workspace-service.js';
import { variableService } from '#lib/services/variable-service.js';
import { extractApiErrorMessage, throwPageLoadError } from '#lib/utils/api.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

async function loadGlobalVariables(queryClient: QueryClient) {
	const result = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.variables.list(),
			queryFn: () => variableService.list()
		})
	);
	if (result.error !== null) return { globalVariables: [], globalVariablesError: extractApiErrorMessage(result.error) };
	return { globalVariables: result.data, globalVariablesError: null };
}

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient, user } = await parent();
	const envId = await resolveDetailEnvironmentId(url);

	type ProjectData = Awaited<ReturnType<typeof projectService.getProjectForEnvironment>>;

	let project: ProjectData;
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.projects.detail(envId, params.projectId),
				queryFn: () => projectService.getProjectForEnvironment(envId, params.projectId)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load project');
	} else {
		project = operationResult.data;
	}

	// Kick off the file-tree walk without awaiting it: on large projects it can
	// take a while and must not block navigation.
	void queryClient.prefetchQuery({
		queryKey: queryKeys.projects.workspace(envId, params.projectId),
		queryFn: () => projectWorkspaceService.getWorkspace(params.projectId, envId)
	});

	const { globalVariables, globalVariablesError } = userHasPermission(user, 'variables:list')
		? await loadGlobalVariables(queryClient)
		: { globalVariables: [], globalVariablesError: null };

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
