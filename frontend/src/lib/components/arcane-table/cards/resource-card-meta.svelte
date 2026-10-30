<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { IconType } from '#lib/icons/index.js';

	let {
		label,
		icon: Icon,
		showLabel = true,
		children
	}: { label: string; icon?: IconType; showLabel?: boolean; children: Snippet } = $props();
</script>

<span class="inline-flex min-w-0 items-center gap-1.5" title={showLabel ? undefined : label}>
	{#if Icon}
		<Icon class="size-3.5 shrink-0 opacity-70" aria-label={label} />
	{/if}
	{#if showLabel}
		<span class="shrink-0 whitespace-nowrap">{label}</span>
	{/if}
	<span class="flex min-w-0 items-center truncate text-foreground/70">{@render children()}</span>
</span>
