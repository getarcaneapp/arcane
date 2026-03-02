import { projectService } from '$lib/services/project-service';
import { templateService } from '$lib/services/template-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import type { QueryClient } from '@tanstack/svelte-query';
import type { PageLoad } from './$types';

async function loadGlobalVariables(queryClient: QueryClient) {
	return queryClient
		.fetchQuery({
			queryKey: queryKeys.templates.globalVariables(),
			queryFn: () => templateService.getGlobalVariables()
		})
		.catch((err) => {
			console.warn('Failed to load global variables:', err);
			return [];
		});
}

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	type ProjectData = Awaited<ReturnType<typeof projectService.getProjectForEnvironment>>;

	let project: ProjectData;
	try {
		project = await queryClient.fetchQuery({
			queryKey: queryKeys.projects.detail(envId, params.projectId),
			queryFn: () => projectService.getProjectForEnvironment(envId, params.projectId)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load project');
	}

	const globalVariables = await loadGlobalVariables(queryClient);

	const editorState = {
		name: project.name || '',
		composeContent: project.composeContent || '',
		envContent: project.envContent || '',
		originalName: project.name || '',
		originalComposeContent: project.composeContent || '',
		originalEnvContent: project.envContent || ''
	};

	return {
		projectId: params.projectId,
		project,
		editorState,
		globalVariables,
		error: null as string | null
	};
};
