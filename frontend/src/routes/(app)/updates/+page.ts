import { userHasPermission } from '#lib/utils/auth';
import { containerService, type ContainerListRequestOptions } from '#lib/services/container-service';
import { projectService } from '#lib/services/project-service';
import { settingsService } from '#lib/services/settings-service';
import { queryKeys } from '#lib/query/query-keys';
import type { SearchPaginationSortRequest } from '#lib/types/shared';
import { resolveInitialTableRequest } from '#lib/utils/tables';
import { throwPageLoadError } from '#lib/utils/api';
import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '#lib/utils/docker';
import type { PageLoad } from './$types';
import { environmentStore } from '#lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();
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
			userHasPermission(user, 'containers:list', envId)
				? queryClient.fetchQuery({
						queryKey: queryKeys.containers.list(envId, containerRequestOptions),
						queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
					})
				: undefined,
			userHasPermission(user, 'projects:list', envId)
				? queryClient.fetchQuery({
						queryKey: queryKeys.projects.list(envId, projectRequestOptions),
						queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions)
					})
				: undefined,
			// `autoUpdateExcludedContainers` drives the ignored state on container rows.
			userHasPermission(user, 'settings:read', envId)
				? queryClient.fetchQuery({
						queryKey: [...queryKeys.settings.byEnvironment(envId), 'updates'],
						queryFn: () => settingsService.getSettingsForEnvironment(envId)
					})
				: undefined
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
