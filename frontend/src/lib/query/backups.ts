import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { s3DestinationService } from '#lib/services/s3-destination-service.js';
import { systemBackupService } from '#lib/services/system-backup-service.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

export function systemBackupHistoryQuery(
	scope: string,
	environments: EnvironmentRef[],
	requestOptions: SearchPaginationSortRequest
) {
	return queryOptions({
		queryKey: queryKeys.systemBackups.history(scope, requestOptions),
		queryFn: () =>
			environments.length > 0
				? fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
						systemBackupService.listHistory(id, request, config)
					)
				: fetchAcrossEnvironments(
						environmentStore.available.filter((env) => env.id === LOCAL_DOCKER_ENVIRONMENT_ID),
						requestOptions,
						(_id, request, config) => systemBackupService.listManagerHistory(request, config)
					)
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
