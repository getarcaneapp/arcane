import { queryOptions, type QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { systemService } from '#lib/services/system-service.js';
import versionService from '#lib/services/version-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { VersionInfo } from '#lib/types/version.js';
import { readEachEnvironment } from '#lib/utils/environments.js';

export function appVersionQuery() {
	return queryOptions({
		queryKey: queryKeys.system.appVersion(),
		queryFn: () => versionService.getVersionInformation()
	});
}

export function latestReleaseQuery() {
	return queryOptions({
		queryKey: queryKeys.system.latestRelease(),
		queryFn: () => versionService.getLatestGitHubRelease()
	});
}

export function upgradeVersionInfoQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.system.versionInfo(environmentId),
		queryFn: () => systemUpgradeService.getVersionInfo(environmentId)
	});
}

export function environmentVersionInfoQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.system.versionInfo(environmentId),
		queryFn: () => environmentManagementService.getVersion(environmentId)
	});
}

export function environmentVersionsQuery(queryClient: QueryClient, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.system.environmentVersions(environments.map((env) => env.id).join(',')),
		queryFn: async () => {
			const versions = await readEachEnvironment<Partial<VersionInfo>>(
				environments,
				(scope, config) => environmentManagementService.getVersion(scope, config),
				{}
			);
			for (const { environment, data } of versions.byEnvironment) {
				queryClient.setQueryData(queryKeys.system.versionInfo(environment.id), data);
			}
			return versions;
		}
	});
}

export function upgradeAvailableQuery(scope: 'mobile-nav' | 'sidebar') {
	return queryOptions({
		queryKey: queryKeys.system.upgradeAvailable(scope),
		queryFn: () => systemUpgradeService.checkUpgradeAvailable()
	});
}

export function environmentUpgradeAvailableQuery(environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.system.environmentUpgradeAvailable(environments.map((env) => env.id).join(',')),
		queryFn: () =>
			readEachEnvironment(environments, (scope, config) => systemUpgradeService.checkUpgradeAvailable(scope, config), {
				canUpgrade: false,
				error: false,
				message: ''
			})
	});
}

export function dockerInfoQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.system.dockerInfo(environmentId),
		queryFn: () => systemService.getDockerInfoForEnvironment(environmentId)
	});
}
