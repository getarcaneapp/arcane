import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { projectService } from '#lib/services/project-service.js';
import { projectWorkspaceService } from '#lib/services/project-workspace-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { ProjectStatusCounts, ProjectTagOption } from '#lib/types/project.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import {
	fetchAcrossEnvironments,
	readAcrossEnvironments,
	sumAcrossEnvironments,
	type EnvironmentFailure
} from '#lib/utils/environments.js';

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

export function projectDetailQuery(environmentId: string, projectId: string) {
	return queryOptions({
		queryKey: queryKeys.projects.detail(environmentId, projectId),
		queryFn: () => projectService.getProjectForEnvironment(environmentId, projectId)
	});
}

export function projectExportOptionsQuery(environmentId: string, projectId: string) {
	return queryOptions({
		queryKey: queryKeys.projects.exportOptions(environmentId, projectId),
		queryFn: () => projectService.getExportOptions(projectId, environmentId),
		staleTime: 0,
		refetchOnWindowFocus: false
	});
}

export function projectDirectoriesQuery(environmentId: string, view: 'projects' | 'settings') {
	return queryOptions({
		queryKey: queryKeys.projects.directories(environmentId, view),
		queryFn: () => projectService.getProjectDirectories(environmentId, view)
	});
}

export function projectWorkspaceFolderQuery(environmentId: string, projectId: string, path: string) {
	return queryOptions({
		queryKey: queryKeys.projects.workspaceFolder(environmentId, projectId, path),
		queryFn: () => projectWorkspaceService.getWorkspace(projectId, path, environmentId)
	});
}

export function projectBackupListQuery(environmentId: string, projectId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.projects.backupList(environmentId, projectId, requestOptions),
		queryFn: async () => {
			await environmentStore.ready;
			return projectService.listBackups(projectId, requestOptions, environmentId);
		}
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

export type ProjectTagOptions = { tags: ProjectTagOption[]; failedEnvironments: EnvironmentFailure[] };

/** Tag options from every shown environment, one per name, with the environments that failed. */
async function fetchProjectTags(environments: EnvironmentRef[]): Promise<ProjectTagOptions> {
	const { value, failures } = await readAcrossEnvironments(environments, (scope, config) =>
		projectService.getProjectTagsForEnvironment(scope, config)
	);
	return { tags: value, failedEnvironments: failures };
}

export function projectTagsQuery(scope: string, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.projects.tags(scope),
		queryFn: () => fetchProjectTags(environments)
	});
}

export function projectListForEnvironmentQuery(environmentId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.projects.list(environmentId, requestOptions),
		queryFn: () => projectService.getProjectsForEnvironment(environmentId, requestOptions)
	});
}
