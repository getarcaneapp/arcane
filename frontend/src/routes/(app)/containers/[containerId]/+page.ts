import { tryCatch } from '#lib/utils/try-catch.js';
import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { containerService } from '#lib/services/container-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { projectService } from '#lib/services/project-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { queryKeys } from '#lib/query/query-keys.js';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const containerId = params.containerId;

	const operationResult = await tryCatch(
		(async () => {
			const [container, settings] = await Promise.all([
				queryClient.query({
					queryKey: queryKeys.containers.detail(envId, containerId),
					queryFn: () => containerService.getContainerForEnvironment(envId, containerId)
				}),
				queryClient.query({
					queryKey: queryKeys.settings.byEnvironment(envId),
					queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
				})
			]);

			if (!container) {
				throw error(404, 'Container not found');
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
				settings,
				project
			};
		})()
	);
	if (operationResult.error !== null) {
		const err: unknown = operationResult.error;

		console.error('Failed to load container:', err);
		if (typeof err === 'object' && err !== null && 'status' in err && (err as { status: number }).status === 404) {
			throw err;
		}
		throw error(500, err instanceof Error ? err.message : 'Failed to load container details');
	} else {
		return operationResult.data;
	}
};
