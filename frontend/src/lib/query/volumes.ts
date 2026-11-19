import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import type { APIRequestConfig } from '#lib/services/api-service.js';
import { settingsService, emptySettings } from '#lib/services/settings-service.js';
import { volumeBackupService } from '#lib/services/volume-backup-service.js';
import { volumeService } from '#lib/services/volume-service.js';
import { volumeWorkspaceService } from '#lib/services/volume-workspace-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { ResolvedSettings } from '#lib/types/ui/settings.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { hasPermission } from '#lib/utils/auth.js';
import {
	environmentRows,
	fetchAcrossEnvironments,
	readAcrossEnvironments,
	readEachEnvironment,
	type EnvironmentFailure
} from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

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

export function volumeBackupPoliciesQuery(environmentId: string, volumeName: string) {
	return queryOptions({
		queryKey: queryKeys.volumeBackupPolicies.list(environmentId, volumeName),
		queryFn: async () => {
			await environmentStore.ready;
			return volumeBackupService.getPolicies(volumeName, environmentId);
		}
	});
}

export function volumeDetailQuery(environmentId: string, volumeName: string) {
	return queryOptions({
		queryKey: queryKeys.volumes.detail(environmentId, volumeName),
		queryFn: () => volumeService.getVolumeForEnvironment(environmentId, volumeName)
	});
}

export function volumeListForEnvironmentQuery(environmentId: string, requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.volumes.table(environmentId, requestOptions),
		queryFn: () => volumeService.getVolumesForEnvironment(environmentId, requestOptions)
	});
}

export function volumeBackupHasPathQuery(environmentId: string, volumeName: string, backupId: string, path: string) {
	return queryOptions({
		queryKey: queryKeys.volumes.backupHasPath(environmentId, volumeName, backupId, path),
		queryFn: () => volumeBackupService.backupHasPath(backupId, `/${path}`, environmentId)
	});
}

async function readBackupVolumeNames(
	environments: EnvironmentRef[],
	fetch: (environmentScope: string, config: APIRequestConfig) => Promise<ResolvedSettings>
): Promise<{ names: [string, string][]; failures: EnvironmentFailure[] }> {
	if (environments.length === 0) return { names: [], failures: [] };
	const result = await tryCatch(readEachEnvironment(environments, fetch, emptySettings));
	if (result.error !== null) {
		const message = extractApiErrorMessage(result.error);
		return { names: [], failures: environments.map((environment) => ({ environment, message })) };
	}
	return {
		names: result.data.byEnvironment.map(({ environment, data }) => [
			environment.id,
			data.runtime.backupVolumeName || 'arcane-backups'
		]),
		failures: result.data.failedEnvironments
	};
}

export function volumeBackupVolumeNamesQuery(scope: string, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.volumes.backupVolumeNames(scope),
		queryFn: async () => {
			const settingsReadable = environments.filter((env) => hasPermission('environment-settings:read', env.id));
			const reads = await Promise.all([
				readBackupVolumeNames(settingsReadable, (environmentScope, config) =>
					settingsService.getSettingsForEnvironment(environmentScope, config)
				),
				readBackupVolumeNames(
					environments.filter((env) => !settingsReadable.includes(env)),
					(environmentScope, config) => settingsService.getPublicSettingsForEnvironment(environmentScope, config)
				)
			]);
			const failures = reads.flatMap((read) => read.failures);
			if (failures.length > 0 && failures.length === environments.length) {
				throw new Error(failures.map(({ environment, message }) => `${environment.name}: ${message}`).join('\n'));
			}
			return { names: new Map(reads.flatMap((read) => read.names)), failures };
		}
	});
}

export function volumeSizesQuery(scope: string, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.volumes.sizes(scope),
		queryFn: async () => {
			const readable = environments.filter((env) => hasPermission('volumes:read', env.id));
			if (readable.length === 0) return [];
			const { value } = await readAcrossEnvironments(readable, (environmentScope, config) =>
				volumeService.getVolumeSizes(environmentScope, config)
			);
			return environmentRows(value, readable);
		}
	});
}
