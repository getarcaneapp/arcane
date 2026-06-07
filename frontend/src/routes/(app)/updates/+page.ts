import { containerService, type ContainerListRequestOptions } from '$lib/services/container-service';
import { projectService } from '$lib/services/project-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '$lib/utils/updates-filter.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

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
	try {
		[containers, projects] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.containers.list(envId, containerRequestOptions),
				queryFn: () => containerService.getContainersForEnvironment(envId, containerRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.projects.list(envId, projectRequestOptions),
				queryFn: () => projectService.getProjectsForEnvironment(envId, projectRequestOptions)
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load updates');
	}

	return {
		envId,
		containers,
		projects,
		containerRequestOptions,
		projectRequestOptions
	};
};
