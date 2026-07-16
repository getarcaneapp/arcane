import { containerService, type ContainerListRequestOptions } from '$lib/services/container-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { projectService } from '$lib/services/project-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import { throwPageLoadError } from '$lib/utils/api';
import { ensureStandaloneContainerUpdatesFilter, ensureUpdatesFilter } from '$lib/utils/docker';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const requestOptions = resolveInitialTableRequest('arcane-updates-workload-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'name', direction: 'asc' }
	} satisfies SearchPaginationSortRequest);
	const sourceLimit = (requestOptions.pagination?.page ?? 1) * (requestOptions.pagination?.limit ?? 20);
	const sourceRequestOptions = {
		...requestOptions,
		pagination: { page: 1, limit: sourceLimit }
	};
	const containerRequestOptions = ensureStandaloneContainerUpdatesFilter(sourceRequestOptions) as ContainerListRequestOptions;
	const projectRequestOptions = ensureUpdatesFilter(sourceRequestOptions);

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
		requestOptions
	};
};
