<script lang="ts">
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { LockIcon } from '$lib/icons';
	import AdaptiveTooltip from '$lib/components/adaptive-tooltip.svelte';

	const active = $derived.by(() => $settingsStore.uiConfigDisabled);
</script>

{#if active}
	<AdaptiveTooltip side="bottom" contentClass="max-w-xs">
		{#snippet trigger()}
			<span
				class="inline-flex items-center gap-1.5 rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800 ring-1 ring-amber-200 dark:bg-amber-900/50 dark:text-amber-200 dark:ring-amber-800"
			>
				<LockIcon class="size-3" />
				Read-only
			</span>
		{/snippet}
		{#snippet content()}
			<div class="space-y-1">
				<p class="font-medium">{m.ui_config_disabled_title()}</p>
				<p class="text-xs opacity-90">{m.ui_config_disabled_description()}</p>
			</div>
		{/snippet}
	</AdaptiveTooltip>
{/if}
