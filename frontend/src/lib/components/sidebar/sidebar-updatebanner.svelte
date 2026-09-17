<script lang="ts">
	import { cn } from '#lib/utils.js';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { useSidebar } from '#lib/components/ui/sidebar/index.js';
	import type { AppVersionInformation } from '#lib/types/settings.js';
	import { m } from '#lib/paraglide/messages.js';
	import UpdateAllDialog from '#lib/components/dialogs/update-all-dialog.svelte';
	import { CircleArrowUpIcon } from '#lib/icons/index.js';
	import { useUpgradeCheck } from '#lib/hooks/use-upgrade-check.svelte.js';
	import UpdateAvailableBanner from './update-available-banner.svelte';

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
	const upgradeCheck = useUpgradeCheck({
		queryScope: 'sidebar',
		getVersionInformation: () => versionInformation,
		getDebug: () => debug
	});

	const tooltipText = $derived(
		m.sidebar_update_available_tooltip({
			version: versionInformation?.newestVersion ?? upgradeCheck.versionChip ?? m.common_unknown()
		})
	);
</script>

<UpdateAllDialog
	bind:open={upgradeCheck.showConfirmDialog}
	{versionInformation}
	canConfirm={upgradeCheck.shouldShowUpgrade}
	debugDemo={debug}
/>

{#if upgradeCheck.shouldShowBanner}
	<div class={cn('pb-1', isCollapsed ? 'px-0' : 'px-3')}>
		{#if !isCollapsed}
			<UpdateAvailableBanner
				label={m.sidebar_update_available()}
				versionChip={upgradeCheck.versionChip}
				disabled={upgradeCheck.checkingUpgrade}
				onclick={upgradeCheck.openDialog}
			/>
		{:else}
			<Tooltip.Root>
				<Tooltip.Trigger>
					{#snippet child({ props })}
						<button
							onclick={upgradeCheck.openDialog}
							disabled={upgradeCheck.checkingUpgrade}
							class="relative mx-auto flex size-8 items-center justify-center rounded-md bg-primary/15 text-primary transition-colors hover:bg-primary/25 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden disabled:cursor-not-allowed disabled:opacity-60"
							{...props}
						>
							<CircleArrowUpIcon class="size-4 shrink-0" aria-hidden="true" />
							<span class="absolute -top-0.5 -right-0.5 size-2 rounded-full bg-primary ring-2 ring-sidebar"></span>
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
