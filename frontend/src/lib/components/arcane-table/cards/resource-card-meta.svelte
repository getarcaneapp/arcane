<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { IconType } from '#lib/icons/index.js';

	let { label, icon: Icon, children }: { label: string; icon?: IconType; children: Snippet } = $props();
</script>

<span class="inline-flex min-w-0 items-center gap-1.5">
	{#if Icon}
		<Icon class="size-3.5 shrink-0 opacity-70" />
	{/if}
	<span>{label}</span>
	<span class="truncate text-foreground/70">{@render children()}</span>
</span>
