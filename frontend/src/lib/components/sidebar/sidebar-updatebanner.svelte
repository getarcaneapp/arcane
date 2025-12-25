<script lang="ts">
	import { cn } from '$lib/utils';
	import * as Separator from '$lib/components/ui/separator/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import type { User } from '$lib/types/user.type';
	import { m } from '$lib/paraglide/messages';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import UpgradeConfirmationDialog from '$lib/components/dialogs/upgrade-confirmation-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { onMount } from 'svelte';
	import { DownloadIcon, ExternalLinkIcon } from '$lib/icons';

	let {
		isCollapsed,
		versionInformation,
		updateAvailable = false,
		debug = false,
		user
	}: {
		isCollapsed: boolean;
		versionInformation?: AppVersionInformation;
		updateAvailable?: boolean;
		debug?: boolean;
		user?: User | null;
	} = $props();

	const sidebar = useSidebar();

	let canUpgrade = $state(false);
	let checkingUpgrade = $state(false);
	let upgrading = $state(false);
	let showConfirmDialog = $state(false);

	const isAdmin = $derived(!!user?.roles?.includes('admin'));
	const shouldShowUpgrade = $derived((canUpgrade && isAdmin) || debug);

	// Determine update type and display text
	const updateType = $derived.by(() => {
		if (!versionInformation) return 'none';
		if (versionInformation.isSemverVersion) return 'semver';
		if (versionInformation.currentTag && versionInformation.newestDigest) return 'digest';
		return 'none';
	});

	const updateDisplayText = $derived.by(() => {
		if (!versionInformation) return '';
		if (updateType === 'semver') {
			return versionInformation.newestVersion ?? '';
		}
		if (updateType === 'digest' && versionInformation.newestDigest) {
			// Show shortened digest for non-semver tags
			const digest = versionInformation.newestDigest;
			return digest.length > 12 ? digest.substring(0, 12) : digest;
		}
		return '';
	});

	const upgradeButtonText = $derived.by(() => {
		if (upgrading) return m.upgrade_in_progress();
		if (checkingUpgrade) return m.upgrade_checking();
		if (updateType === 'digest') {
			return `Update ${versionInformation?.currentTag ?? 'image'}`;
		}
		return m.upgrade_to_version({ version: versionInformation?.newestVersion ?? '' });
	});

	// Debug mode: force show upgrade button
	$effect(() => {
		if (debug) {
			canUpgrade = true;
		}
	});

	// Check if self-upgrade is available
	onMount(() => {
		if (updateAvailable && isAdmin && !debug) {
			checkUpgradeAvailability();
		}
	});

	async function checkUpgradeAvailability() {
		if (checkingUpgrade) return;

		checkingUpgrade = true;
		try {
			const result = await systemUpgradeService.checkUpgradeAvailable();
			canUpgrade = result.canUpgrade && !result.error;
		} catch (error) {
			canUpgrade = false;
		} finally {
			checkingUpgrade = false;
		}
	}

	function handleUpgradeClick() {
		showConfirmDialog = true;
	}

	async function handleConfirmUpgrade() {
		try {
			await systemUpgradeService.triggerUpgrade();
			// Dialog will handle countdown and reload
		} catch (error: any) {
			const errorMessage = error?.response?.data?.error || error?.message || 'Unknown error';
			toast.error(m.upgrade_failed({ error: errorMessage }));
			upgrading = false;
		}
	}

	// Show banner for both semver and digest-based updates
	const shouldShowBanner = $derived(updateAvailable || debug);
</script>

{#snippet updateInfo()}
	<div class="flex flex-col gap-1">
		<span class="text-sm font-semibold">{m.sidebar_update_available()}</span>
		{#if versionInformation?.currentTag}
			<span class="text-xs text-blue-500/60">
				Tracking: {versionInformation.currentTag}
			</span>
		{/if}
		<span class="text-xs text-blue-500/80">
			{#if updateType === 'semver'}
				{m.sidebar_version({ version: versionInformation?.newestVersion ?? m.common_unknown() })}
			{:else if updateType === 'digest'}
				New digest: {updateDisplayText}
			{:else}
				{m.common_unknown()}
			{/if}
		</span>
	</div>
{/snippet}

{#snippet upgradeButton()}
	<ArcaneButton
		action="update"
		size="sm"
		class="h-9 w-full gap-2 rounded-xl shadow-sm transition-colors"
		onclick={handleUpgradeClick}
		disabled={upgrading || checkingUpgrade}
		customLabel={upgradeButtonText}
		icon={DownloadIcon}
	/>
{/snippet}

<UpgradeConfirmationDialog
	bind:open={showConfirmDialog}
	bind:upgrading
	version={versionInformation?.newestVersion ?? ''}
	onConfirm={handleConfirmUpgrade}
/>

{#if shouldShowBanner}
	<div class={cn('pb-2', isCollapsed ? 'px-1' : 'px-4')}>
		<Separator.Root class="mb-3 opacity-30" />

		{#if !isCollapsed}
			<div
				class="rounded-xl border border-blue-500/20 bg-linear-to-br from-blue-500/10 to-blue-600/5 p-3 transition-all hover:scale-[1.02] hover:from-blue-500/15 hover:to-blue-600/10 hover:shadow-md"
			>
				<div class="flex flex-col gap-2">
					<a
						href={versionInformation?.releaseUrl}
						target="_blank"
						rel="noopener noreferrer"
						class="group flex items-center justify-between text-blue-600 transition-colors duration-200 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
					>
						{@render updateInfo()}
						<ExternalLinkIcon class="size-4 transition-transform duration-200 group-hover:scale-110" />
					</a>

					{#if shouldShowUpgrade}
						{@render upgradeButton()}
					{/if}
				</div>
			</div>
		{:else}
			<div class="flex flex-col items-center gap-2">
				<Tooltip.Root>
					<Tooltip.Trigger>
						{#snippet child({ props })}
							<div
								class="flex h-8 w-8 items-center justify-center rounded-lg border border-blue-500/20 bg-linear-to-br from-blue-500/10 to-blue-600/5 transition-all hover:scale-[1.02] hover:from-blue-500/15 hover:to-blue-600/10 hover:shadow-md"
								{...props}
							>
								<a
									href={versionInformation?.releaseUrl}
									target="_blank"
									rel="noopener noreferrer"
									class="flex h-full w-full items-center justify-center text-blue-600 transition-all duration-200 hover:scale-110 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
								>
									<ExternalLinkIcon />
								</a>
							</div>
						{/snippet}
					</Tooltip.Trigger>
					<Tooltip.Content side="right" align="center" hidden={sidebar.state !== 'collapsed' || sidebar.isHovered}>
						{m.sidebar_update_available_tooltip({
							version: versionInformation?.newestVersion ?? m.common_unknown()
						})}
					</Tooltip.Content>
				</Tooltip.Root>

				{#if shouldShowUpgrade}
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}
								<button
									onclick={handleUpgradeClick}
									disabled={upgrading || checkingUpgrade}
									class="border-primary/20 bg-primary/15 text-primary hover:bg-primary/25 focus-visible:ring-primary/40 dark:text-primary flex size-8 items-center justify-center rounded-lg border transition-all hover:scale-[1.02] hover:shadow-md focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
									{...props}
								>
									<DownloadIcon class="size-3.5" />
								</button>
							{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content side="right" align="center" hidden={sidebar.state !== 'collapsed' || sidebar.isHovered}>
							{upgradeButtonText}
						</Tooltip.Content>
					</Tooltip.Root>
				{/if}
			</div>
		{/if}
	</div>
{/if}
