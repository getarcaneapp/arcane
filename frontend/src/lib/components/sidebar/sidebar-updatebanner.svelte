<script lang="ts">
	import { cn } from '$lib/utils';
	import * as Separator from '$lib/components/ui/separator/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import UpdateCenterDialog from '$lib/components/dialogs/update-center-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { DownloadIcon } from '$lib/icons';
	import { extractApiErrorMessage } from '$lib/utils/api';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { hasPermission } from '$lib/utils/auth';

	let {
		isCollapsed,
		versionInformation,
		debug = false
	}: {
		isCollapsed: boolean;
		versionInformation?: AppVersionInformation;
		debug?: boolean;
	} = $props();

	const sidebar = useSidebar();

	let upgrading = $state(false);
	let showConfirmDialog = $state(false);
	const canInstallUpdates = $derived(hasPermission('environments:update'));

	const shouldCheckUpgrade = $derived(!!(versionInformation?.updateAvailable && canInstallUpdates && !debug));
	const upgradeAvailabilityQuery = createQuery(() => ({
		queryKey: queryKeys.system.upgradeAvailable('sidebar'),
		queryFn: () => systemUpgradeService.checkUpgradeAvailable(),
		enabled: shouldCheckUpgrade,
		staleTime: 0
	}));

	const canUpgrade = $derived.by(() => {
		if (debug) return true;
		const result = upgradeAvailabilityQuery.data;
		return !!result?.canUpgrade && !result?.error;
	});
	const checkingUpgrade = $derived(
		!!(shouldCheckUpgrade && (upgradeAvailabilityQuery.isPending || upgradeAvailabilityQuery.isFetching))
	);
	const shouldShowUpgrade = $derived((canUpgrade && canInstallUpdates) || debug);

	const updateType = $derived.by(() => {
		if (!versionInformation) return 'none';
		if (versionInformation.isSemverVersion) return 'semver';
		if (versionInformation.currentTag && versionInformation.newestDigest) return 'digest';
		return 'none';
	});

	const versionChip = $derived.by(() => {
		if (!versionInformation) return '';
		if (updateType === 'semver') return versionInformation.newestVersion ?? '';
		if (updateType === 'digest') return versionInformation.currentTag ?? '';
		return '';
	});

	const tooltipText = $derived(
		m.sidebar_update_available_tooltip({
			version: versionInformation?.newestVersion ?? versionChip ?? m.common_unknown()
		})
	);

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

	function handleConfirmUpgrade() {
		triggerUpgradeMutation.mutate();
	}

	const shouldShowBanner = $derived(versionInformation?.updateAvailable || debug);
</script>

<UpdateCenterDialog
	bind:open={showConfirmDialog}
	bind:upgrading
	{versionInformation}
	canInstall={shouldShowUpgrade}
	{debug}
	onConfirm={handleConfirmUpgrade}
/>

{#if shouldShowBanner}
	<div class={cn('pb-2', isCollapsed ? 'px-1' : 'px-3')}>
		<Separator.Root class="mb-2 opacity-30" />

		{#if !isCollapsed}
			<button
				type="button"
				onclick={openDialog}
				disabled={upgrading || checkingUpgrade}
				class="group hover:bg-muted/50 focus-visible:ring-primary/40 flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
			>
				<span class="relative flex size-2 shrink-0 items-center justify-center">
					<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-60"></span>
					<span class="relative inline-flex size-1.5 rounded-full bg-blue-500"></span>
				</span>
				<span class="text-foreground flex-1 text-sm font-medium">
					{m.sidebar_update_available()}
				</span>
				{#if versionChip}
					<span class="bg-muted text-muted-foreground rounded-md px-1.5 py-0.5 font-mono text-[11px]">
						{versionChip}
					</span>
				{/if}
			</button>
		{:else}
			<Tooltip.Root>
				<Tooltip.Trigger>
					{#snippet child({ props })}
						<button
							onclick={openDialog}
							disabled={upgrading || checkingUpgrade}
							class="hover:bg-muted/60 focus-visible:ring-primary/40 relative mx-auto flex size-8 items-center justify-center rounded-lg transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
							{...props}
						>
							<DownloadIcon class="text-foreground/80 size-4" />
							<span class="absolute top-1 right-1 flex size-2">
								<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-70"></span>
								<span class="relative inline-flex size-2 rounded-full bg-blue-500 ring-2 ring-[var(--sidebar)]"></span>
							</span>
						</button>
					{/snippet}
				</Tooltip.Trigger>
				<Tooltip.Content side="right" align="center" hidden={sidebar.state !== 'collapsed' || sidebar.isHovered}>
					{tooltipText}
				</Tooltip.Content>
			</Tooltip.Root>
		{/if}
	</div>
{/if}
