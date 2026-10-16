import { error } from '@sveltejs/kit';

import { queryKeys } from '#lib/query/query-keys.js';
import { containerService } from '#lib/services/container-service.js';
import { projectService } from '#lib/services/project-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { queryClient } = await parent();
	const envId = await resolveDetailEnvironmentId(url);
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
				const findByName = async (options: SearchPaginationSortRequest) => {
					const projectsResult = await queryClient.query({
						queryKey: queryKeys.projects.list(envId, options),
						queryFn: () => projectService.getProjectsForEnvironment(envId, options)
					});
					return projectsResult.data.find((p) => p.name === composeProjectName);
				};
				// Agents without the name filter ignore it, so fall back to the searched lookup.
				const matched =
					(await findByName({ filters: { name: composeProjectName }, pagination: { page: 1, limit: 1 } })) ??
					(await findByName({ search: composeProjectName, pagination: { page: 1, limit: 100 } }));
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
		envId,
		container,
		project
	};
};
