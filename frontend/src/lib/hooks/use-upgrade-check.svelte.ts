import { createQuery, keepPreviousData, useQueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import type { VersionInfo } from '#lib/types/version.js';
import { hasPermission } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { sumAcrossEnvironments } from '#lib/utils/environments.js';

type UseUpgradeCheckOptions = {
	queryScope: 'mobile-nav' | 'sidebar';
	getVersionInformation: () => VersionInfo | undefined;
	getDebug?: () => boolean;
};

export function useUpgradeCheck({ queryScope, getVersionInformation, getDebug = () => false }: UseUpgradeCheckOptions) {
	let showConfirmDialog = $state(false);

	// Same permission the update-all endpoint enforces (PermSystemUpgrade).
	const canInstallUpdates = $derived(hasPermission('system:upgrade', LOCAL_DOCKER_ENVIRONMENT_ID));
	const queryClient = useQueryClient();
	const agentVersionsQuery = createQuery(() => {
		const environments = environmentStore.available.filter(
			(env) => env.id !== LOCAL_DOCKER_ENVIRONMENT_ID && isEnvironmentOnline(env) && hasPermission('environments:read', env.id)
		);
		return {
			queryKey: queryKeys.system.environmentVersions(environments.map((env) => env.id).join(',')),
			queryFn: async () => {
				const versions = await sumAcrossEnvironments<Partial<VersionInfo>>(
					environments,
					(scope, config) => environmentManagementService.getVersion(scope, config),
					{}
				);
				for (const { environment, data } of versions.byEnvironment) {
					queryClient.setQueryData(queryKeys.system.versionInfo(environment.id), data);
				}
				return versions;
			},
			enabled: environments.length > 0,
			placeholderData: keepPreviousData,
			staleTime: 5 * 60_000,
			refetchOnWindowFocus: false,
			retry: false
		};
	});
	const agentUpdates = $derived(
		(agentVersionsQuery.data?.byEnvironment ?? []).flatMap(({ data }) => (data.updateAvailable ? [data] : []))
	);
	const updateCount = $derived(agentUpdates.length + (getVersionInformation()?.updateAvailable ? 1 : 0));
	const shouldCheckUpgrade = $derived(
		!!(updateCount > 0 && canInstallUpdates && hasPermission('system:read', LOCAL_DOCKER_ENVIRONMENT_ID) && !getDebug())
	);
	const upgradeAvailabilityQuery = createQuery(() => ({
		queryKey: queryKeys.system.upgradeAvailable(queryScope),
		queryFn: () => systemUpgradeService.checkUpgradeAvailable(),
		enabled: shouldCheckUpgrade,
		staleTime: 0
	}));

	const canUpgrade = $derived.by(() => {
		if (getDebug()) return true;
		const result = upgradeAvailabilityQuery.data;
		return !!result?.canUpgrade && !result?.error;
	});
	const checkingUpgrade = $derived(
		!!(shouldCheckUpgrade && (upgradeAvailabilityQuery.isPending || upgradeAvailabilityQuery.isFetching))
	);
	const shouldShowUpgrade = $derived((canUpgrade && canInstallUpdates) || getDebug());

	const updateType = $derived.by(() => {
		const versionInformation = getVersionInformation();
		if (!versionInformation) return 'none';
		if (versionInformation.isSemverVersion) return 'semver';
		if (versionInformation.currentTag && versionInformation.newestDigest) return 'digest';
		return 'none';
	});

	const versionChip = $derived.by(() => {
		const versionInformation = getVersionInformation();
		if (!versionInformation) return '';
		if (updateType === 'semver') return versionInformation.newestVersion ?? '';
		if (updateType === 'digest') return versionInformation.currentTag ?? '';
		if (getDebug()) return versionInformation.newestVersion || 'v9.9.9';
		return agentUpdates[0]?.newestVersion ?? '';
	});

	const shouldShowBanner = $derived(updateCount > 0 || getDebug());

	function openDialog() {
		showConfirmDialog = true;
	}

	return {
		get showConfirmDialog() {
			return showConfirmDialog;
		},
		set showConfirmDialog(value: boolean) {
			showConfirmDialog = value;
		},
		get canInstallUpdates() {
			return canInstallUpdates;
		},
		get checkingUpgrade() {
			return checkingUpgrade;
		},
		get shouldShowUpgrade() {
			return shouldShowUpgrade;
		},
		get versionChip() {
			return versionChip;
		},
		get shouldShowBanner() {
			return shouldShowBanner;
		},
		get updateCount() {
			return updateCount;
		},
		openDialog
	};
}
