import { projectService } from '$lib/services/project-service';
import { templateService } from '$lib/services/template-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const [project, globalVariables] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.projects.detail(envId, params.projectId),
			queryFn: () => projectService.getProjectForEnvironment(envId, params.projectId)
		}),
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.globalVariables(),
				queryFn: () => templateService.getGlobalVariables()
			})
			.catch((err) => {
				console.warn('Failed to load global variables:', err);
				return [];
			})
	]);

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
		error: null
	};
};
