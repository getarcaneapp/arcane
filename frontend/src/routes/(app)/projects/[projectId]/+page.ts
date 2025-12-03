import { projectService } from '$lib/services/project-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params }) => {
	const project = await projectService.getProject(params.projectId);

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
		error: null
	};
};
