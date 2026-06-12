<script lang="ts">
	import { cn } from '$lib/utils';
	import * as Separator from '$lib/components/ui/separator/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { m } from '$lib/paraglide/messages';
	import UpdateCenterDialog from '$lib/components/dialogs/update-center-dialog.svelte';
	import { DownloadIcon } from '$lib/icons';
	import { useUpgradeCheck } from '$lib/hooks/use-upgrade-check.svelte';

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

<UpdateCenterDialog
	bind:open={upgradeCheck.showConfirmDialog}
	bind:upgrading={upgradeCheck.upgrading}
	{versionInformation}
	canInstall={upgradeCheck.shouldShowUpgrade}
	{debug}
	onConfirm={upgradeCheck.confirmUpgrade}
/>

{#if upgradeCheck.shouldShowBanner}
	<div class={cn('pb-2', isCollapsed ? 'px-1' : 'px-3')}>
		<Separator.Root class="mb-2 opacity-30" />

		{#if !isCollapsed}
			<button
				type="button"
				onclick={upgradeCheck.openDialog}
				disabled={upgradeCheck.upgrading || upgradeCheck.checkingUpgrade}
				class="group hover:bg-muted/50 focus-visible:ring-primary/40 flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
			>
				<span class="relative flex size-2 shrink-0 items-center justify-center">
					<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-60"></span>
					<span class="relative inline-flex size-1.5 rounded-full bg-blue-500"></span>
				</span>
				<span class="text-foreground flex-1 text-sm font-medium">
					{m.sidebar_update_available()}
				</span>
				{#if upgradeCheck.versionChip}
					<span class="bg-muted text-muted-foreground rounded-md px-1.5 py-0.5 font-mono text-[11px]">
						{upgradeCheck.versionChip}
					</span>
				{/if}
			</button>
		{:else}
			<Tooltip.Root>
				<Tooltip.Trigger>
					{#snippet child({ props })}
						<button
							onclick={upgradeCheck.openDialog}
							disabled={upgradeCheck.upgrading || upgradeCheck.checkingUpgrade}
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
