import { m } from '$lib/paraglide/messages';
import { queryKeys } from '$lib/query/query-keys';
import systemUpgradeService from '$lib/services/api/system-upgrade-service';
import type { AppVersionInformation } from '$lib/types/settings';
import { extractApiErrorMessage } from '$lib/utils/api';
import { hasPermission } from '$lib/utils/auth';
import { createMutation, createQuery } from '@tanstack/svelte-query';
import { toast } from 'svelte-sonner';

type UseUpgradeCheckOptions = {
	queryScope: 'mobile-nav' | 'sidebar';
	getVersionInformation: () => AppVersionInformation | undefined;
	getDebug?: () => boolean;
};

export function useUpgradeCheck({ queryScope, getVersionInformation, getDebug = () => false }: UseUpgradeCheckOptions) {
	let upgrading = $state(false);
	let showConfirmDialog = $state(false);

	const canInstallUpdates = $derived(hasPermission('environments:update'));
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
	const triggerUpgradeMutation = createMutation(() => ({
		mutationFn: () => systemUpgradeService.triggerUpgrade(),
		onError: (error: unknown) => {
			const errorMessage = extractApiErrorMessage(error);
			const wrappedPrefix = m.upgrade_failed({ error: '' });
			toast.error(errorMessage.startsWith(wrappedPrefix) ? errorMessage : m.upgrade_failed({ error: errorMessage }));
			upgrading = false;
		}
	}));

	function openDialog() {
		showConfirmDialog = true;
	}

	function confirmUpgrade() {
		triggerUpgradeMutation.mutate();
	}

	return {
		get upgrading() {
			return upgrading;
		},
		set upgrading(value: boolean) {
			upgrading = value;
		},
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
		openDialog,
		confirmUpgrade
	};
}
