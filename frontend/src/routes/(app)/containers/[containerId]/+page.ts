import { tryCatch } from '#lib/utils/try-catch.js';
import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { containerService } from '#lib/services/container-service.js';
import { projectService } from '#lib/services/project-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { throwPageLoadError } from '#lib/utils/api.js';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const containerId = params.containerId;

	const containerResult = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.containers.detail(envId, containerId),
			queryFn: () => containerService.getContainerForEnvironment(envId, containerId)
		})
	);
	if (containerResult.error !== null) {
		throwPageLoadError(containerResult.error, 'Failed to load container details');
	}
	const container = containerResult.data;
	if (!container) {
		error(404, 'Container not found');
	}

	let project = null;
	const composeProjectName = container.composeInfo?.projectName;
	if (composeProjectName) {
		const operationResult = await tryCatch(
			(async () => {
				const searchOptions = {
					search: composeProjectName,
					pagination: { page: 1, limit: 100 } // Ensure we don't miss projects beyond default page size
				};
				const projectsResult = await queryClient.query({
					queryKey: queryKeys.projects.list(envId, searchOptions),
					queryFn: () => projectService.getProjectsForEnvironment(envId, searchOptions)
				});
				const matched = projectsResult.data.find((p) => p.name === composeProjectName);
				if (matched) {
					return await queryClient.query({
						queryKey: queryKeys.projects.detail(envId, matched.id),
						queryFn: () => projectService.getProjectForEnvironment(envId, matched.id)
					});
				}
				return null;
			})()
		);
		if (operationResult.error !== null) {
			const err = operationResult.error;

			console.warn('Failed to load compose project:', err);
		} else {
			project = operationResult.data;
		}
	}

	return {
		container,
		project
	};
};
