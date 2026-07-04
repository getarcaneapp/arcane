<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { m } from '$lib/paraglide/messages';
	import UpdateAllDialog from '$lib/components/dialogs/update-all-dialog.svelte';
	import { DownloadIcon } from '$lib/icons';
	import { useUpgradeCheck } from '$lib/hooks/use-upgrade-check.svelte';

	let {
		versionInformation,
		debug = false
	}: {
		versionInformation?: AppVersionInformation;
		debug?: boolean;
	} = $props();

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

<UpdateAllDialog bind:open={upgradeCheck.showConfirmDialog} {versionInformation} canConfirm={upgradeCheck.shouldShowUpgrade} />

{#if upgradeCheck.shouldShowBanner}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<button
					onclick={upgradeCheck.openDialog}
					disabled={upgradeCheck.checkingUpgrade}
					class="hover:bg-sidebar-accent/60 focus-visible:ring-primary/40 relative flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					{...props}
				>
					<DownloadIcon class="text-foreground/80 size-4" />
					<span class="absolute top-1.5 right-1.5 flex size-2">
						<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-70"></span>
						<span class="relative inline-flex size-2 rounded-full bg-blue-500"></span>
					</span>
				</button>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content side="bottom" align="center">
			{tooltipText}
		</Tooltip.Content>
	</Tooltip.Root>
{/if}
