import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { volumeBackupService } from '#lib/services/volume-backup-service.js';
import { volumeService } from '#lib/services/volume-service.js';
import { volumeWorkspaceService } from '#lib/services/volume-workspace-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function volumeListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.volumes.table(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				volumeService.getVolumesForEnvironment(id, request, config)
			)
	});
}

export function volumeBackupListQuery(environmentId: string, volumeName: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.volumes.backupList(environmentId, volumeName, requestOptions),
		queryFn: async () => {
			await environmentStore.ready;
			return volumeBackupService.listBackups(volumeName, requestOptions, environmentId);
		}
	});
}

export function volumeWorkspaceFolderQuery(environmentId: string, volumeName: string, path: string) {
	return queryOptions({
		queryKey: queryKeys.volumes.workspaceFolder(environmentId, volumeName, path),
		queryFn: () => volumeWorkspaceService.getWorkspace(volumeName, path, environmentId)
	});
}
