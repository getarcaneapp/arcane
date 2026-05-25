<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { createQuery } from '@tanstack/svelte-query';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import UpdateCenterDialog from '$lib/components/dialogs/update-center-dialog.svelte';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import environmentUpgradeService from '$lib/services/api/environment-upgrade-service';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import type { AppVersionInformation } from '$lib/types/settings';
	import type { Environment } from '$lib/types/environment';
	import { extractApiErrorMessage } from '$lib/utils/api';
	import { DownloadIcon } from '$lib/icons';

	let {
		environment,
		versionInfo,
		canUpgrade: canUpgradePermission,
		debug = false,
		onRefreshRequested,
		render = 'both',
		open = $bindable(false),
		upgrading = $bindable(false)
	}: {
		environment: Environment;
		versionInfo: AppVersionInformation;
		canUpgrade: boolean;
		debug?: boolean;
		onRefreshRequested?: () => void | Promise<void>;
		render?: 'both' | 'trigger' | 'dialog';
		open?: boolean;
		upgrading?: boolean;
	} = $props();

	const shouldCheckUpgrade = $derived(!!(versionInfo.updateAvailable && canUpgradePermission && !debug));
	const isLocalEnvironment = $derived(environment.id === '0');

	const upgradeAvailabilityQuery = createQuery(() => ({
		queryKey: queryKeys.system.environmentUpgradeAvailable(environment.id),
		queryFn: () =>
			environment.id === '0'
				? systemUpgradeService.checkUpgradeAvailable()
				: environmentUpgradeService.checkEnvironmentUpgradeAvailable(environment.id),
		enabled: shouldCheckUpgrade,
		staleTime: 0
	}));

	const upgradeIsAvailable = $derived.by(() => {
		if (debug) return true;
		const result = upgradeAvailabilityQuery.data;
		return !!result?.canUpgrade && !result?.error;
	});

	const checkingUpgrade = $derived(
		!!(shouldCheckUpgrade && (upgradeAvailabilityQuery.isPending || upgradeAvailabilityQuery.isFetching))
	);

	const updateType = $derived.by(() => {
		if (versionInfo.isSemverVersion) return 'semver';
		if (versionInfo.currentTag && versionInfo.newestDigest) return 'digest';
		return 'none';
	});

	const updateDisplayText = $derived.by(() => {
		if (updateType === 'semver') {
			return versionInfo.newestVersion ?? '';
		}

		if (updateType === 'digest' && versionInfo.newestDigest) {
			const digest = versionInfo.newestDigest;
			return digest.length > 12 ? digest.slice(0, 12) : digest;
		}

		return '';
	});

	const shouldShowUpgrade = $derived(
		(versionInfo.updateAvailable && canUpgradePermission && upgradeIsAvailable) || (debug && canUpgradePermission)
	);

	const upgradeButtonText = $derived.by(() => {
		if (upgrading) return m.upgrade_in_progress();
		if (checkingUpgrade) return m.upgrade_checking();

		if (updateType === 'digest') {
			const tag = versionInfo.currentTag ?? m.common_image();
			return m.upgrade_update_tag({ tag });
		}

		const version = versionInfo.newestVersion || updateDisplayText;
		if (version) {
			return m.upgrade_to_version({ version });
		}

		return m.upgrade_now();
	});

	async function handleConfirmUpgradeInternal() {
		if (isLocalEnvironment) {
			try {
				await systemUpgradeService.triggerUpgrade();
				toast.success(m.upgrade_success());
			} catch (error) {
				const errorMessage = extractApiErrorMessage(error);
				toast.error(m.upgrade_failed({ error: errorMessage }));
				throw error;
			}
			return;
		}

		try {
			const result = await environmentUpgradeService.triggerEnvironmentUpgrade(environment.id);
			if (!result.success) {
				throw new Error(result.error || result.message || m.common_unknown());
			}
			toast.success(m.upgrade_success());
			await onRefreshRequested?.();
		} catch (error) {
			const errorMessage = extractApiErrorMessage(error);
			const wrappedPrefix = m.upgrade_failed({ error: '' });
			toast.error(errorMessage.startsWith(wrappedPrefix) ? errorMessage : m.upgrade_failed({ error: errorMessage }));
			throw error;
		}
	}
</script>

{#if shouldShowUpgrade && render !== 'dialog'}
	<ArcaneButton
		action="update"
		size="sm"
		class="shrink-0"
		onclick={() => (open = true)}
		disabled={upgrading || checkingUpgrade}
		customLabel={upgradeButtonText}
		icon={DownloadIcon}
	/>
{/if}

{#if render !== 'trigger'}
	<UpdateCenterDialog
		bind:open
		bind:upgrading
		versionInformation={versionInfo}
		canInstall={shouldShowUpgrade}
		environmentName={isLocalEnvironment ? undefined : environment.name}
		environmentId={environment.id}
		onConfirm={handleConfirmUpgradeInternal}
	/>
{/if}
