import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service.js';
import { projectService } from '#lib/services/project-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker.js';
import type { PageLoad } from './$types';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const containerRequestOptions = ensureStandaloneContainerUpdatesFilter(
		resolveInitialTableRequest('arcane-updates-container-table', {
			pagination: { page: 1, limit: 100 },
			sort: { column: 'created', direction: 'desc' }
		} satisfies SearchPaginationSortRequest)
	) as ContainerListRequestOptions;

	const projectRequestOptions = ensureUpdatesFilter(
		resolveInitialTableRequest('arcane-updates-project-table', {
			pagination: { page: 1, limit: 20 },
			sort: { column: 'name', direction: 'asc' }
		} satisfies SearchPaginationSortRequest)
	);

	let containers;
	let projects;
	let settings;
	try {
		[containers, projects, settings] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.containers.list(envId, containerRequestOptions),
				queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.projects.list(envId, projectRequestOptions),
				queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions)
			}),
			// `autoUpdateExcludedContainers` drives the ignored state on container rows.
			queryClient.fetchQuery({
				queryKey: queryKeys.settings.byEnvironment(envId),
				queryFn: () => settingsService.getSettingsForEnvironmentMerged(envId)
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load updates');
	}

	return {
		envId,
		containers,
		projects,
		settings,
		containerRequestOptions,
		projectRequestOptions
	};
};
