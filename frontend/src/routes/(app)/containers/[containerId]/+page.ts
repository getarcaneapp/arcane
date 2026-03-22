import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';
import { containerService } from '$lib/services/container-service';
import { settingsService } from '$lib/services/settings-service';
import { projectService } from '$lib/services/project-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const containerId = params.containerId;

	try {
		const [container, settings] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.containers.detail(envId, containerId),
				queryFn: () => containerService.getContainerForEnvironment(envId, containerId)
			}),
			queryClient.fetchQuery({
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
			try {
				const searchOptions = {
					search: composeProjectName,
					pagination: { page: 1, limit: 100 } // Ensure we don't miss projects beyond default page size
				};
				const projectsResult = await queryClient.fetchQuery({
					queryKey: queryKeys.projects.list(envId, searchOptions),
					queryFn: () => projectService.getProjectsForEnvironment(envId, searchOptions)
				});
				const matched = projectsResult.data.find((p) => p.name === composeProjectName);
				if (matched) {
					project = await queryClient.fetchQuery({
						queryKey: queryKeys.projects.detail(envId, matched.id),
						queryFn: () => projectService.getProjectForEnvironment(envId, matched.id)
					});
				}
			} catch (err) {
				console.warn('Failed to load compose project:', err);
			}
		}

		return {
			container,
			settings,
			project
		};
	} catch (err: unknown) {
		console.error('Failed to load container:', err);
		if (typeof err === 'object' && err !== null && 'status' in err && (err as { status: number }).status === 404) {
			throw err;
		}
		throw error(500, err instanceof Error ? err.message : 'Failed to load container details');
	}
};
