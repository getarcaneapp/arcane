import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { projectService } from '#lib/services/project-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import type { ProjectStatusCounts } from '#lib/types/swarm.js';
import { fetchAcrossEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';

const emptyStatusCounts: ProjectStatusCounts = {
	runningProjects: 0,
	stoppedProjects: 0,
	totalProjects: 0,
	archivedProjects: 0,
	updatesAvailable: 0
};

export function projectListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.projects.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				projectService.getProjectsForEnvironment(id, request, config)
			)
	});
}

export function projectStatusCountsQuery(scope: string, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.projects.statusCounts(scope),
		queryFn: () =>
			sumAcrossEnvironments(
				environments,
				(id, config) => projectService.getProjectStatusCountsForEnvironment(id, config),
				emptyStatusCounts
			)
	});
}
