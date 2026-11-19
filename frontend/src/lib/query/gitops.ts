import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function gitOpsSyncListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				gitOpsSyncService.getSyncs(id, request, config)
			)
	});
}

export function gitOpsSyncListForEnvironmentQuery(environmentId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.list(environmentId, requestOptions),
		queryFn: () => gitOpsSyncService.getSyncs(environmentId, requestOptions)
	});
}

export function gitOpsSyncDetailQuery(environmentId: string, syncId: string | undefined) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.detail(environmentId, syncId ?? 'none'),
		queryFn: () => gitOpsSyncService.getSync(environmentId, syncId ?? '')
	});
}

export function gitOpsProjectBackupQuery(environmentId: string, projectId: string) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.projectBackup(environmentId, projectId),
		queryFn: () =>
			gitOpsSyncService.getSyncs(environmentId, {
				pagination: { page: 1, limit: 1 },
				filters: { projectId, mode: 'backup,bidirectional' }
			})
	});
}

export function gitOpsBackupHistoryQuery(environmentId: string, syncId: string, limit?: number) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.backupHistory(environmentId, syncId, limit),
		queryFn: () => gitOpsSyncService.getBackupHistory(environmentId, syncId, limit)
	});
}

export function gitOpsBackupRevisionQuery(environmentId: string, syncId: string, commit: string) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.backupRevision(environmentId, syncId, commit),
		queryFn: () => gitOpsSyncService.getBackupRevision(environmentId, syncId, commit)
	});
}

export function gitOpsBackupPreviewQuery(environmentId: string, syncId: string) {
	return queryOptions({
		queryKey: queryKeys.gitOpsSyncs.backupPreview(environmentId, syncId),
		queryFn: () => gitOpsSyncService.previewBackup(environmentId, syncId)
	});
}
