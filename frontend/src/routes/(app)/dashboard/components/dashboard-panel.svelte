<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as Card from '#lib/components/ui/card/index.js';

	let {
		title,
		actions,
		children
	}: {
		title: string;
		actions?: Snippet;
		children: Snippet;
	} = $props();
</script>

<Card.Root variant="subtle" role="region" aria-label={title} class="flex h-full min-w-0 flex-col gap-3 border-border/30 p-5">
	<div class="flex min-h-7 items-center justify-between gap-2">
		<h2 class="truncate text-sm font-medium">{title}</h2>
		{@render actions?.()}
	</div>
	{@render children()}
</Card.Root>
