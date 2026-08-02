import { queryKeys } from '#lib/query/query-keys';
import systemUpgradeService from '#lib/services/api/system-upgrade-service';
import type { AppVersionInformation } from '#lib/types/settings';
import { hasPermission } from '#lib/utils/auth';
import { createQuery } from '@tanstack/svelte-query';

type UseUpgradeCheckOptions = {
	queryScope: 'mobile-nav' | 'sidebar';
	getVersionInformation: () => AppVersionInformation | undefined;
	getDebug?: () => boolean;
};

export function useUpgradeCheck({ queryScope, getVersionInformation, getDebug = () => false }: UseUpgradeCheckOptions) {
	let showConfirmDialog = $state(false);

	// Same permission the update-all endpoint enforces (PermSystemUpgrade).
	const canInstallUpdates = $derived(hasPermission('system:upgrade'));
	const shouldCheckUpgrade = $derived(!!(getVersionInformation()?.updateAvailable && canInstallUpdates && !getDebug()));
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
		return '';
	});

	const shouldShowBanner = $derived(getVersionInformation()?.updateAvailable || getDebug());

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
		openDialog
	};
}
