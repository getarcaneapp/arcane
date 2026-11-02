import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { s3DestinationService } from '#lib/services/s3-destination-service.js';
import { systemBackupService } from '#lib/services/system-backup-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function systemBackupHistoryQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.systemBackups.history(requestOptions),
		queryFn: () => systemBackupService.listHistory(requestOptions)
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
