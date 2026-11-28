import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { activityService } from '#lib/services/activity-service.js';
import { s3DestinationService } from '#lib/services/s3-destination-service.js';
import { systemBackupService } from '#lib/services/system-backup-service.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { Activity } from '#lib/types/activity.js';
import type { BackupStorageStats } from '#lib/types/backup.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { hasPermission } from '#lib/utils/auth.js';
import { fetchAcrossEnvironments, readEachEnvironment } from '#lib/utils/environments.js';

export function systemBackupHistoryQuery(scope: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.systemBackups.history(scope, requestOptions),
		queryFn: () => {
			// Saved backup records need no Docker, so the manager stays listed even when its local Docker is off.
			const environments = environmentStore.enabled.filter((env) => hasPermission('volumes:read', env.id));
			if (environments.length > 0) {
				return fetchAcrossEnvironments(
					environments,
					requestOptions,
					(id, request, config) => systemBackupService.listHistory(id, request, config),
					environmentStore.enabled
				);
			}
			return fetchAcrossEnvironments(
				environmentStore.available.filter((env) => env.id === LOCAL_DOCKER_ENVIRONMENT_ID),
				requestOptions,
				(_id, request, config) => systemBackupService.listManagerHistory(request, config),
				environmentStore.enabled
			);
		}
	});
}

export function backupStorageStatsQuery(scope: string, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.systemBackups.storageStats(scope),
		queryFn: () =>
			readEachEnvironment(environments, (id, config) => systemBackupService.getStorageStats(id, config), {
				repositories: []
			} satisfies BackupStorageStats)
	});
}

export function systemBackupPoliciesQuery() {
	return queryOptions({
		queryKey: queryKeys.systemBackups.policies(),
		queryFn: () => systemBackupService.getPolicies()
	});
}

export function systemVolumeBackupConfigQuery() {
	return queryOptions({
		queryKey: queryKeys.systemBackups.systemVolumeConfig(),
		queryFn: () => systemBackupService.getSystemVolumeConfig()
	});
}

export function projectBackupConfigQuery() {
	return queryOptions({
		queryKey: queryKeys.systemBackups.projectConfig(),
		queryFn: () => systemBackupService.getProjectConfig()
	});
}

export function projectBackupOptionsQuery() {
	return queryOptions({
		queryKey: queryKeys.systemBackups.projectOptions(),
		queryFn: () => systemBackupService.listProjectOptions()
	});
}

export function s3DestinationListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.s3Destinations.list(requestOptions),
		queryFn: () => s3DestinationService.list(requestOptions)
	});
}

export function s3DestinationAllQuery() {
	return queryOptions({
		queryKey: queryKeys.s3Destinations.listAll(),
		queryFn: () => s3DestinationService.listAll()
	});
}

export function systemVolumeBackupOptionsQuery() {
	return queryOptions({
		queryKey: queryKeys.systemBackups.systemVolumeOptions(),
		queryFn: () => systemBackupService.listSystemVolumeOptions()
	});
}

export function backupActivityIdsQuery(
	environmentId: string,
	resourceKey: string,
	matches: (activity: Activity) => boolean,
	discover: ((environmentId: string) => Promise<string[]>) | undefined
) {
	return queryOptions({
		queryKey: queryKeys.backupActivities.active(environmentId, resourceKey, !!discover),
		queryFn: async ({ signal }): Promise<string[]> => {
			await environmentStore.ready;
			signal.throwIfAborted();
			if (discover) return discover(environmentId);
			const activities: Activity[] = [];
			for (const status of ['queued', 'running']) {
				let page = 1;
				let hasNextPage = false;
				do {
					const result = await activityService.getActivities(
						{ pagination: { page, limit: 100 }, filters: { status } },
						environmentId
					);
					signal.throwIfAborted();
					activities.push(...result.data);
					hasNextPage = result.pagination.hasNextPage ?? false;
					page++;
				} while (hasNextPage);
			}
			return activities.filter(matches).map((activity) => activity.id);
		}
	});
}
